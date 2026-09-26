package httpapi

// TEMPORARY DEBUG CODE - remove this entire file with debug_ids.go.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"social-notes/backend/internal/store"
)

func newDebugIDsTestClient(t *testing.T, accountID string) *testClient {
	t.Helper()
	resetDebugWebhookIDs()
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := Handler(db, Options{
		InstagramAccountID: accountID, InstagramUsername: "debug-inbox",
		InstagramWebhookVerifyToken: "verify", InstagramAppSecret: "app-secret",
	})
	c := &testClient{handler: h, db: db}
	c.register(t, "Debug Owner", "owner@example.com")
	if _, err := c.db.Exec(`UPDATE instagram_integrations SET owner_user_id=(SELECT id FROM users WHERE email='owner@example.com') WHERE instagram_user_id=?`, accountID); err != nil {
		t.Fatal(err)
	}
	return c
}

func postDebugWebhook(t *testing.T, c *testClient, payload string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, body = %s", w.Code, w.Body.String())
	}
}

func getDebugIDs(t *testing.T, c *testClient) map[string]any {
	t.Helper()
	res := c.request(t, http.MethodGet, "/api/integrations/instagram/debug/ids", nil, false)
	if res.Code != http.StatusOK {
		t.Fatalf("debug status = %d, body = %s", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDebugIDsRequiresAuthentication(t *testing.T) {
	c := newDebugIDsTestClient(t, "debug-inbox")
	anon := &testClient{handler: c.handler, db: c.db}
	res := anon.request(t, http.MethodGet, "/api/integrations/instagram/debug/ids", nil, false)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestDebugIDsNoWebhookCaptured(t *testing.T) {
	c := newDebugIDsTestClient(t, "debug-inbox")
	body := getDebugIDs(t, c)
	if body["captured"] != false || body["webhook"] != nil || body["recipient_matches_configured"] != false {
		t.Fatalf("empty body = %v", body)
	}
}

func TestDebugIDsWebhookRecipientSenderCaptured(t *testing.T) {
	c := newDebugIDsTestClient(t, "debug-inbox")
	postDebugWebhook(t, c, `{"object":"instagram","entry":[{"id":"debug-inbox","messaging":[{"sender":{"id":"debug-sender"},"recipient":{"id":"debug-inbox"},"message":{"mid":"mid-1","text":"hello"}}]}]}`)
	body := getDebugIDs(t, c)
	if body["captured"] != true {
		t.Fatalf("captured = %v", body)
	}
	webhook, _ := body["webhook"].(map[string]any)
	if webhook["recipient_id"] != "debug-inbox" || webhook["sender_id"] != "debug-sender" || webhook["message_mid"] != "mid-1" || webhook["timestamp"] == "" {
		t.Fatalf("webhook = %v", webhook)
	}
}

func TestDebugIDsRecipientMatchesConfigured(t *testing.T) {
	c := newDebugIDsTestClient(t, "debug-inbox")
	postDebugWebhook(t, c, `{"object":"instagram","entry":[{"id":"debug-inbox","messaging":[{"sender":{"id":"debug-sender"},"recipient":{"id":"debug-inbox"},"message":{"mid":"mid-1","text":"hello"}}]}]}`)
	body := getDebugIDs(t, c)
	if body["recipient_matches_configured"] != true {
		t.Fatalf("match body = %v", body)
	}
	configured, _ := body["configured"].(map[string]any)
	if configured["recipient_id"] != "debug-inbox" {
		t.Fatalf("configured = %v", configured)
	}
}

func TestDebugIDsRecipientDoesNotMatchConfigured(t *testing.T) {
	c := newDebugIDsTestClient(t, "debug-inbox")
	postDebugWebhook(t, c, `{"object":"instagram","entry":[{"id":"other-inbox","messaging":[{"sender":{"id":"debug-sender"},"recipient":{"id":"other-inbox"},"message":{"mid":"mid-1","text":"hello"}}]}]}`)
	body := getDebugIDs(t, c)
	if body["captured"] != true || body["recipient_matches_configured"] != false {
		t.Fatalf("mismatch body = %v", body)
	}
	webhook, _ := body["webhook"].(map[string]any)
	if webhook["recipient_id"] != "other-inbox" {
		t.Fatalf("webhook = %v", webhook)
	}
}
