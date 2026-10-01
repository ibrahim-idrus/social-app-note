package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"social-notes/backend/internal/store"
)

func newFacebookIdentityClient(t *testing.T, configured bool) *testClient {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	options := Options{InstagramAccountID: "selected-account", InstagramUsername: "notedesk_inbox", InstagramAccessToken: "token"}
	if configured {
		options.FacebookPageID = "page-123"
	}
	return &testClient{handler: Handler(db, options), db: db}
}

func TestFacebookPlatformHiddenUntilFullyConfigured(t *testing.T) {
	for _, options := range []Options{{}} {
		db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
		if err != nil {
			t.Fatal(err)
		}
		c := &testClient{handler: Handler(db, options), db: db}
		c.register(t, "Alice", "alice-"+strings.NewReplacer("/", "-").Replace(t.Name())+"@example.com")
		body := c.request(t, http.MethodGet, "/api/social-platforms", nil, false).Body.String()
		if strings.Contains(body, `"id":"facebook"`) || strings.Contains(body, "page-secret") {
			t.Fatalf("partial config exposed Facebook: %s", body)
		}
		db.Close()
	}

	c := newFacebookIdentityClient(t, true)
	c.register(t, "Alice", "alice@example.com")
	body := c.request(t, http.MethodGet, "/api/social-platforms", nil, false).Body.String()
	if !strings.Contains(body, `"id":"facebook"`) || !strings.Contains(body, `"page_id":"page-123"`) {
		t.Fatalf("configured Facebook platform missing: %s", body)
	}

}

func TestFacebookIdentityAuthenticationCSRFCreateListRegenerateAndRemove(t *testing.T) {
	c := newFacebookIdentityClient(t, true)
	if got := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "facebook"}, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create=%d", got)
	}
	c.register(t, "Alice", "alice@example.com")
	if got := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "facebook"}, false).Code; got != http.StatusForbidden {
		t.Fatalf("create without CSRF=%d", got)
	}
	created := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "facebook", "username": "Alice Example"}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"username":"Alice Example"`) {
		t.Fatalf("Facebook username was not persisted: %s", created.Body.String())
	}
	var registration struct {
		ID                  int64  `json:"id"`
		Platform            string `json:"platform"`
		VerificationCode    string `json:"verification_code"`
		VerificationExpires string `json:"verification_expires_at"`
		VerificationState   string `json:"verification_state"`
		FacebookPage        struct {
			PageID string `json:"page_id"`
		} `json:"facebook_page"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	if registration.Platform != "facebook" || registration.VerificationCode == "" || registration.VerificationExpires == "" || registration.VerificationState != "waiting" || registration.FacebookPage.PageID != "page-123" {
		t.Fatalf("registration=%#v", registration)
	}
	listed := c.request(t, http.MethodGet, "/api/social-identities", nil, false)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"facebook_page":{"page_id":"page-123"}`) || strings.Contains(listed.Body.String(), registration.VerificationCode) {
		t.Fatalf("listed=%d %s", listed.Code, listed.Body.String())
	}

	path := "/api/social-identities/" + itoa(int(registration.ID)) + "/verification-code"
	if got := c.request(t, http.MethodPost, path, nil, false).Code; got != http.StatusForbidden {
		t.Fatalf("regenerate without CSRF=%d", got)
	}
	regenerated := c.request(t, http.MethodPost, path, nil, true)
	if regenerated.Code != http.StatusOK || !strings.Contains(regenerated.Body.String(), `"platform":"facebook"`) {
		t.Fatalf("regenerate=%d %s", regenerated.Code, regenerated.Body.String())
	}
	if err := json.Unmarshal(regenerated.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if outcome, err := store.ProcessFacebookMessage(context.Background(), c.db, "page-123", "psid-1", "bind-1", registration.VerificationCode, false, nil, now); err != nil || outcome.Kind != store.FacebookMessageActivated {
		t.Fatalf("binding=%#v err=%v", outcome, err)
	}
	listed = c.request(t, http.MethodGet, "/api/social-identities", nil, false)
	if !strings.Contains(listed.Body.String(), `"verification_state":"active"`) {
		t.Fatalf("active status missing: %s", listed.Body.String())
	}
	var notes int
	if err := c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil || notes != 0 {
		t.Fatalf("binding created notes=%d err=%v", notes, err)
	}
	deletePath := "/api/social-identities/" + itoa(int(registration.ID))
	if got := c.request(t, http.MethodDelete, deletePath, nil, false).Code; got != http.StatusForbidden {
		t.Fatalf("delete without CSRF=%d", got)
	}
	if got := c.request(t, http.MethodDelete, deletePath, nil, true).Code; got != http.StatusNoContent {
		t.Fatalf("delete=%d", got)
	}
}

func TestFacebookUnavailableCannotBeCreatedAndInstagramContractIsUnchanged(t *testing.T) {
	c := newFacebookIdentityClient(t, false)
	c.register(t, "Alice", "alice@example.com")
	res := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{"platform": "facebook"}, true)
	if res.Code != http.StatusBadRequest || res.Body.String() != "{\"error\":\"invalid_input\"}\n" {
		t.Fatalf("unconfigured create=%d %s", res.Code, res.Body.String())
	}
	instagram := registerInstagram(t, c, "alice")
	if instagram.Username != "alice" || instagram.InstagramAccount.Username != "notedesk_inbox" || instagram.VerificationCode == "" {
		t.Fatalf("Instagram registration changed: %#v", instagram)
	}
}
