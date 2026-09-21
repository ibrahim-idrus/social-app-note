package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"social-notes/backend/internal/store"
)

func webhookHandler(t *testing.T, verifyToken, appSecret string) (http.Handler, *testClient) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := Handler(db, Options{InstagramAccountID: "inbox", InstagramUsername: "akun_testing911", InstagramAccessToken: "token", InstagramWebhookVerifyToken: verifyToken, InstagramAppSecret: appSecret})
	return h, &testClient{handler: h, db: db}
}

func TestInstagramWebhookVerification(t *testing.T) {
	h, _ := webhookHandler(t, "verify-secret", "app-secret")
	for _, tc := range []struct {
		name, query string
		want        int
		body        string
	}{
		{"valid", "hub.mode=subscribe&hub.verify_token=verify-secret&hub.challenge=challenge-123", 200, "challenge-123"},
		{"bad token", "hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=challenge-123", 403, ""},
		{"bad mode", "hub.mode=wrong&hub.verify_token=verify-secret&hub.challenge=challenge-123", 403, ""},
		{"missing challenge", "hub.mode=subscribe&hub.verify_token=verify-secret", 403, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/integrations/instagram/webhook?"+tc.query, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || (tc.body != "" && w.Body.String() != tc.body) || strings.Contains(w.Body.String(), "verify-secret") {
				t.Fatalf("got %d %q", w.Code, w.Body.String())
			}
		})
	}
	h2, _ := webhookHandler(t, "", "app-secret")
	w := httptest.NewRecorder()
	h2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/integrations/instagram/webhook?hub.mode=subscribe&hub.verify_token=x&hub.challenge=y", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing config got %d", w.Code)
	}
}

func signWebhook(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func TestInstagramWebhookSignatureAndPayloadValidation(t *testing.T) {
	h, _ := webhookHandler(t, "verify", "app-secret")
	valid := []byte(`{"object":"instagram","entry":[]}`)
	for _, tc := range []struct {
		name, signature string
		body            []byte
		want            int
	}{
		{"valid", signWebhook("app-secret", valid), valid, 200},
		{"missing", "", valid, 403},
		{"bad", signWebhook("wrong", valid), valid, 403},
		{"malformed", "sha256=nope", valid, 403},
		{"json", signWebhook("app-secret", []byte(`{`)), []byte(`{`), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(string(tc.body)))
			r.Header.Set("X-Hub-Signature-256", tc.signature)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || strings.Contains(w.Body.String(), "app-secret") {
				t.Fatalf("got %d %q", w.Code, w.Body.String())
			}
		})
	}
}

func TestInstagramWebhookProcessesSignedTextAndIgnoresEcho(t *testing.T) {
	h, c := webhookHandler(t, "verify", "app-secret")
	c.register(t, "Alice", "alice@example.com")
	registration := registerInstagram(t, c, "alice")
	payload := `{"object":"instagram","entry":[{"id":"inbox","messaging":[{"sender":{"id":"sender-1"},"recipient":{"id":"inbox"},"message":{"mid":"mid-1","text":"` + registration.VerificationCode + `"}},{"sender":{"id":"sender-1"},"recipient":{"id":"inbox"},"message":{"mid":"mid-echo","text":"ignored","is_echo":true}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var status, platformID string
	if err := c.db.QueryRow(`SELECT status, platform_user_id FROM social_identities WHERE id=?`, registration.ID).Scan(&status, &platformID); err != nil || status != "active" || platformID != "sender-1" {
		t.Fatalf("status=%s id=%s err=%v", status, platformID, err)
	}
	var notes int
	_ = c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes)
	if notes != 0 {
		t.Fatalf("echo created note: %d", notes)
	}
}
