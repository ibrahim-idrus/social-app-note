package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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

func TestSocialPlatformsExposeOnlyDisabledInstagramCapability(t *testing.T) {
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
	if len(result.Platforms) != 1 || result.Platforms[0].ID != "instagram" || result.Platforms[0].Available || result.Platforms[0].SearchEnabled {
		t.Fatalf("platforms = %#v", result.Platforms)
	}
	if got := c.request(t, http.MethodGet, "/api/social-identities/search?platform=instagram&username=alice", nil, false).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("search endpoint exists: status = %d", got)
	}
	if got := c.request(t, http.MethodPut, "/api/social-identities/1", map[string]string{}, true).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("identity PUT exists: status = %d", got)
	}
	if got := c.request(t, http.MethodPost, "/api/integrations/instagram/webhook", map[string]string{}, false).Code; got != http.StatusNotFound {
		t.Fatalf("webhook endpoint exists: status = %d", got)
	}
}

func TestPendingInstagramIdentityCreateListNormalizeAndValidate(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")

	if got := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": "alice",
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
	if stableID.Code != http.StatusBadRequest || stableID.Body.String() != "{\"error\":\"invalid_json\"}\n" {
		t.Fatalf("stable-ID create = %d %q", stableID.Code, stableID.Body.String())
	}

	created := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": " @Alice.Name_1 ",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var identity map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	if identity["platform"] != "instagram" || identity["username"] != "alice.name_1" || identity["normalized_username"] != "alice.name_1" || identity["status"] != "pending" || identity["platform_user_id"] != nil {
		t.Fatalf("identity = %#v", identity)
	}
	if _, exists := identity["user_id"]; exists {
		t.Fatalf("identity leaks user_id: %#v", identity)
	}

	listed := c.request(t, http.MethodGet, "/api/social-identities", nil, false)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"username":"alice.name_1"`) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
}

func TestIdentityConflictsAreExactAndPrivate(t *testing.T) {
	a := newTestClient(t, false)
	a.register(t, "Alice", "alice@example.com")
	created := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": "alice",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}

	occupied := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": "someone_else",
	}, true)
	if occupied.Code != http.StatusConflict || occupied.Body.String() != "{\"error\":\"platform_identity_already_registered\"}\n" {
		t.Fatalf("occupied slot = %d %q", occupied.Code, occupied.Body.String())
	}

	b := newTestClientWithHandler(t, a.handler)
	b.register(t, "Bob", "bob@example.com")
	unavailable := b.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": " @ALICE ",
	}, true)
	if unavailable.Code != http.StatusConflict || unavailable.Body.String() != "{\"error\":\"identity_unavailable\"}\n" {
		t.Fatalf("unavailable = %d %q", unavailable.Code, unavailable.Body.String())
	}
	if strings.Contains(strings.ToLower(unavailable.Body.String()), "alice@example.com") || strings.Contains(unavailable.Body.String(), `"user_id"`) {
		t.Fatalf("owner leaked: %s", unavailable.Body.String())
	}
	bCreated := b.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": "bob",
	}, true)
	if bCreated.Code != http.StatusCreated {
		t.Fatalf("user B create = %d %s", bCreated.Code, bCreated.Body.String())
	}
	bothConflicts := a.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": "bob",
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
		"platform": "instagram", "username": "alice",
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
