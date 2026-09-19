package httpapi

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"social-notes/backend/internal/store"
)

func TestNormalizeInstagramUsername(t *testing.T) {
	for input, want := range map[string]string{
		"Alice":           "alice",
		" @Alice.Name_1 ": "alice.name_1",
	} {
		got, ok := normalizeInstagramUsername(input)
		if !ok || got != want {
			t.Fatalf("normalizeInstagramUsername(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}

	for _, input := range []string{
		"", "@", "@@alice", ".alice", "alice.", "alice..name", "alice-name", "alice name",
		strings.Repeat("a", 31), "álîce",
	} {
		if got, ok := normalizeInstagramUsername(input); ok {
			t.Fatalf("normalizeInstagramUsername(%q) = %q, true; want invalid", input, got)
		}
	}
}

func TestInstagramSearchUsesAuthenticatedOwnerEndpointAndExactNormalizedMatch(t *testing.T) {
	var requests int
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	handler := Handler(db, Options{
		InstagramAccountID:    "must-not-be-used",
		InstagramAccessToken:  "secret-token",
		InstagramGraphVersion: "v99.0",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "graph.instagram.com" || req.URL.Path != "/v99.0/me" {
				t.Fatalf("provider request = %s %s", req.Method, req.URL)
			}
			if got := req.URL.Query().Get("fields"); got != "id,username,name,profile_picture_url" {
				t.Fatalf("fields = %q", got)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer secret-token" {
				t.Fatalf("authorization = %q", got)
			}
			if req.URL.Query().Has("access_token") || strings.Contains(req.URL.String(), "must-not-be-used") || strings.Contains(req.URL.String(), "business_discovery") {
				t.Fatalf("unsafe provider request = %s", req.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"owner-id","username":"Owner.Name","name":"Owner","profile_picture_url":"https://cdn.example/owner.jpg"}`)), Header: make(http.Header)}, nil
		})},
	})
	c := newTestClientWithHandler(t, handler)
	c.db = db
	c.register(t, "Owner", "owner@example.com")

	matched := c.request(t, http.MethodGet, "/api/social-identities/search?username=%20%40owner.name%20", nil, false)
	if matched.Code != http.StatusOK || !strings.Contains(matched.Body.String(), `"accounts":[{"id":"owner-id","username":"Owner.Name"`) {
		t.Fatalf("matched search = %d %s", matched.Code, matched.Body.String())
	}
	mismatched := c.request(t, http.MethodGet, "/api/social-identities/search?username=someone_else", nil, false)
	if mismatched.Code != http.StatusOK || mismatched.Body.String() != "{\"accounts\":[]}\n" {
		t.Fatalf("mismatched search = %d %s", mismatched.Code, mismatched.Body.String())
	}
	if requests != 2 {
		t.Fatalf("provider requests = %d, want 2", requests)
	}
}

func TestInstagramSearchReturnsSanitizedStableProviderErrors(t *testing.T) {
	tests := []struct {
		name, body, wantCode string
		status               int
	}{
		{"oauth", `{"error":{"message":"token secret detail","type":"OAuthException","code":190,"fbtrace_id":"trace-secret"}}`, "instagram_auth_failed", http.StatusUnauthorized},
		{"non-json", `upstream secret raw body`, "instagram_search_failed", http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			handler := Handler(db, Options{InstagramAccessToken: "secret-token", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}})
			c := newTestClientWithHandler(t, handler)
			c.db = db
			c.register(t, "Alice", test.name+"@example.com")
			res := c.request(t, http.MethodGet, "/api/social-identities/search?username=alice", nil, false)
			if res.Code != http.StatusBadGateway || res.Body.String() != "{\"error\":\""+test.wantCode+"\"}\n" {
				t.Fatalf("search = %d %s", res.Code, res.Body.String())
			}
			for _, secret := range []string{"secret-token", "token secret detail", "trace-secret", "upstream secret raw body"} {
				if strings.Contains(res.Body.String(), secret) {
					t.Fatalf("response leaked %q: %s", secret, res.Body.String())
				}
			}
		})
	}
}

func TestInstagramIdentityConfirmationRevalidatesAuthenticatedOwner(t *testing.T) {
	tests := []struct {
		name, response, id, username string
		status                       int
		wantError                    string
	}{
		{"correct", `{"id":"ig-1","username":"alice","name":"Alice","profile_picture_url":"https://cdn.example/alice.jpg"}`, "ig-1", "alice", http.StatusCreated, ""},
		{"missing", `{}`, "ig-1", "alice", http.StatusConflict, "instagram_account_changed"},
		{"changed id", `{"id":"ig-2","username":"alice"}`, "ig-1", "alice", http.StatusConflict, "instagram_account_changed"},
		{"changed username", `{"id":"ig-1","username":"alice.new"}`, "ig-1", "alice", http.StatusConflict, "instagram_account_changed"},
		{"stale provider failure", `provider unavailable`, "ig-1", "alice", http.StatusBadGateway, "instagram_search_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			providerStatus := http.StatusOK
			if test.name == "stale provider failure" {
				providerStatus = http.StatusBadGateway
			}
			api := Handler(db, Options{InstagramAccountID: "selected-account", InstagramAccessToken: "token", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "graph.instagram.com" || !strings.HasSuffix(req.URL.Path, "/me") {
					t.Fatalf("provider request = %s", req.URL)
				}
				return &http.Response{StatusCode: providerStatus, Body: io.NopCloser(strings.NewReader(test.response)), Header: make(http.Header)}, nil
			})}})
			c := newTestClientWithHandler(t, api)
			c.db = db
			c.register(t, "Alice", strings.ReplaceAll(test.name, " ", "-")+"@example.com")
			res := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "instagram", "platform_user_id": test.id, "username": test.username}, true)
			if res.Code != test.status {
				t.Fatalf("create = %d %s", res.Code, res.Body.String())
			}
			if test.wantError != "" && !strings.Contains(res.Body.String(), `"error":"`+test.wantError+`"`) {
				t.Fatalf("error = %s", res.Body.String())
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM social_identities`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			wantCount := 0
			if test.status == http.StatusCreated {
				wantCount = 1
			}
			if count != wantCount {
				t.Fatalf("identities = %d, want %d", count, wantCount)
			}
			if wantCount == 1 {
				var id, username, displayName, avatar string
				if err := db.QueryRow(`SELECT platform_user_id, username, display_name, avatar_url FROM social_identities`).Scan(&id, &username, &displayName, &avatar); err != nil {
					t.Fatal(err)
				}
				if id != "ig-1" || username != "alice" || displayName != "Alice" || avatar == "" {
					t.Fatalf("stored = %q %q %q %q", id, username, displayName, avatar)
				}
			}
		})
	}
}

func TestInstagramIdentityConfirmationRejectsTypedUsernameAndDuplicates(t *testing.T) {
	requests := 0
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	handler := Handler(db, Options{InstagramAccountID: "selected-account", InstagramAccessToken: "token", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"ig-1","username":"alice"}`)), Header: make(http.Header)}, nil
	})}})
	a := newTestClientWithHandler(t, handler)
	a.db = db
	a.register(t, "Alice", "alice@example.com")
	typed := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "instagram", "username": "alice"}, true)
	if typed.Code != http.StatusBadRequest || requests != 0 {
		t.Fatalf("typed username = %d, provider requests %d", typed.Code, requests)
	}
	if got := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "instagram", "platform_user_id": "ig-1", "username": "alice"}, true).Code; got != http.StatusCreated {
		t.Fatalf("first create = %d", got)
	}
	b := newTestClientWithHandler(t, handler)
	b.db = db
	b.register(t, "Bob", "bob@example.com")
	duplicate := b.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "instagram", "platform_user_id": "ig-1", "username": "alice"}, true)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "identity_unavailable") {
		t.Fatalf("duplicate = %d %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestSocialPlatformsAndIdentitiesRequireAuthentication(t *testing.T) {
	c := newTestClient(t, false)
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/social-platforms"},
		{http.MethodGet, "/api/social-identities"},
		{http.MethodPost, "/api/social-identities"},
		{http.MethodDelete, "/api/social-identities/1"},
	} {
		if got := c.request(t, request.method, request.path, map[string]string{}, false).Code; got != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d", request.method, request.path, got)
		}
	}
}

func TestSocialPlatformsExposeInstagramPrototypeCapability(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")
	res := c.request(t, http.MethodGet, "/api/social-platforms", nil, false)
	if res.Code != http.StatusOK {
		t.Fatalf("platforms = %d %s", res.Code, res.Body.String())
	}
	var result struct {
		Platforms []struct {
			ID            string `json:"id"`
			Available     bool   `json:"available"`
			SearchEnabled bool   `json:"search_enabled"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Platforms) != 1 || result.Platforms[0].ID != "instagram" || !result.Platforms[0].Available || !result.Platforms[0].SearchEnabled {
		t.Fatalf("platforms = %#v", result.Platforms)
	}
	if got := c.request(t, http.MethodGet, "/api/social-identities/search?platform=instagram&username=alice", nil, false).Code; got != http.StatusOK {
		t.Fatalf("configured search = %d", got)
	}
	if got := c.request(t, http.MethodPut, "/api/social-identities/1", map[string]string{}, true).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("identity PUT exists: status = %d", got)
	}
	if got := c.request(t, http.MethodPost, "/api/integrations/instagram/webhook", map[string]string{}, false).Code; got != http.StatusNotFound {
		t.Fatalf("webhook endpoint exists: status = %d", got)
	}
}

func TestInstagramSearchCapabilityDependsOnlyOnAccessToken(t *testing.T) {
	for _, test := range []struct {
		name, token string
		want        bool
	}{
		{"token present without account id", "token", true},
		{"token absent", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			c := newTestClientWithHandler(t, Handler(db, Options{InstagramAccessToken: test.token}))
			c.db = db
			c.register(t, "Alice", strings.ReplaceAll(test.name, " ", "-")+"@example.com")
			res := c.request(t, http.MethodGet, "/api/social-platforms", nil, false)
			var result struct {
				Platforms []struct {
					SearchEnabled bool `json:"search_enabled"`
				} `json:"platforms"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Platforms) != 1 || result.Platforms[0].SearchEnabled != test.want {
				t.Fatalf("platforms = %#v, want search_enabled %v", result.Platforms, test.want)
			}
		})
	}
}

func TestPendingInstagramIdentityCreateListNormalizeAndValidate(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")

	if got := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-alice", "username": "alice",
	}, false).Code; got != http.StatusForbidden {
		t.Fatalf("create without CSRF = %d", got)
	}

	for _, body := range []map[string]string{
		{"platform": "facebook", "username": "alice"},
		{"platform": "instagram", "username": "alice..name"},
		{"platform": "instagram", "username": "@@alice"},
	} {
		res := c.request(t, http.MethodPost, "/api/social-identities", body, true)
		if res.Code != http.StatusBadRequest || res.Body.String() != "{\"error\":\"invalid_input\"}\n" {
			t.Fatalf("invalid create %#v = %d %q", body, res.Code, res.Body.String())
		}
	}
	stableID := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": "alice", "platform_user_id": "unvalidated",
	}, true)
	if stableID.Code != http.StatusConflict || stableID.Body.String() != "{\"error\":\"instagram_account_changed\"}\n" {
		t.Fatalf("stable-ID create = %d %q", stableID.Code, stableID.Body.String())
	}

	created := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-alice", "username": " @Alice ",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var identity map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	if identity["platform"] != "instagram" || identity["username"] != "alice" || identity["normalized_username"] != "alice" || identity["status"] != "pending" || identity["platform_user_id"] != "ig-alice" {
		t.Fatalf("identity = %#v", identity)
	}
	if _, exists := identity["user_id"]; exists {
		t.Fatalf("identity leaks user_id: %#v", identity)
	}

	listed := c.request(t, http.MethodGet, "/api/social-identities", nil, false)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"username":"alice"`) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
}

func TestIdentityConflictsAreExactAndPrivate(t *testing.T) {
	a := newTestClient(t, false)
	a.register(t, "Alice", "alice@example.com")
	created := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-alice", "username": "alice",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}

	occupied := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-alice", "username": "alice",
	}, true)
	if occupied.Code != http.StatusConflict || occupied.Body.String() != "{\"error\":\"platform_identity_already_registered\"}\n" {
		t.Fatalf("occupied slot = %d %q", occupied.Code, occupied.Body.String())
	}

	bHandler := Handler(a.db, Options{InstagramAccessToken: "token", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"ig-bob","username":"bob"}`)), Header: make(http.Header)}, nil
	})}})
	b := newTestClientWithHandler(t, a.handler)
	b.db = a.db
	b.register(t, "Bob", "bob@example.com")
	unavailable := b.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-alice", "username": " @ALICE ",
	}, true)
	if unavailable.Code != http.StatusConflict || unavailable.Body.String() != "{\"error\":\"identity_unavailable\"}\n" {
		t.Fatalf("unavailable = %d %q", unavailable.Code, unavailable.Body.String())
	}
	if strings.Contains(strings.ToLower(unavailable.Body.String()), "alice@example.com") || strings.Contains(unavailable.Body.String(), `"user_id"`) {
		t.Fatalf("owner leaked: %s", unavailable.Body.String())
	}
	b.handler = bHandler
	bCreated := b.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-bob", "username": "bob",
	}, true)
	if bCreated.Code != http.StatusCreated {
		t.Fatalf("user B create = %d %s", bCreated.Code, bCreated.Body.String())
	}
	aWithBobProvider := *a
	aWithBobProvider.handler = bHandler
	bothConflicts := aWithBobProvider.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-bob", "username": "bob",
	}, true)
	if bothConflicts.Code != http.StatusConflict || bothConflicts.Body.String() != "{\"error\":\"platform_identity_already_registered\"}\n" {
		t.Fatalf("occupied slot precedence = %d %q", bothConflicts.Code, bothConflicts.Body.String())
	}
	listed := b.request(t, http.MethodGet, "/api/social-identities", nil, false)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"username":"bob"`) || strings.Contains(listed.Body.String(), `"username":"alice"`) {
		t.Fatalf("user B list = %d %q", listed.Code, listed.Body.String())
	}
}

func TestDeleteIdentityIsCSRFProtectedOwnedAndPreservesNotes(t *testing.T) {
	a := newTestClient(t, false)
	a.register(t, "Alice", "alice@example.com")
	created := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "platform_user_id": "ig-alice", "username": "alice",
	}, true)
	var identity map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	id := int(identity["id"].(float64))
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE email = 'alice@example.com'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	result, err := a.db.Exec(`INSERT INTO notes (user_id, social_identity_id, title, content_markdown, source, external_message_id) VALUES (?, ?, 'DM', 'keep me', 'instagram', 'event-1')`, userID, id)
	if err != nil {
		t.Fatal(err)
	}
	noteID, _ := result.LastInsertId()

	if got := a.request(t, http.MethodDelete, "/api/social-identities/"+itoa(id), nil, false).Code; got != http.StatusForbidden {
		t.Fatalf("delete without CSRF = %d", got)
	}
	b := newTestClientWithHandler(t, a.handler)
	b.register(t, "Bob", "bob@example.com")
	if got := b.request(t, http.MethodDelete, "/api/social-identities/"+itoa(id), nil, true).Code; got != http.StatusNotFound {
		t.Fatalf("other user delete = %d", got)
	}
	if got := a.request(t, http.MethodDelete, "/api/social-identities/"+itoa(id), nil, true).Code; got != http.StatusNoContent {
		t.Fatalf("owner delete = %d", got)
	}
	var socialIdentityID sql.NullInt64
	var content string
	if err := a.db.QueryRow(`SELECT social_identity_id, content_markdown FROM notes WHERE id = ?`, noteID).Scan(&socialIdentityID, &content); err != nil {
		t.Fatal(err)
	}
	if socialIdentityID.Valid || content != "keep me" {
		t.Fatalf("preserved note = identity %#v, content %q", socialIdentityID, content)
	}
}
