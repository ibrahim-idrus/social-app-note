package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
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

func TestInstagramWebhookSavesUnmatchedTextToInboxOwner(t *testing.T) {
	_, c := webhookHandler(t, "verify", "app-secret")
	c.register(t, "Inbox Owner", "owner@example.com")
	if _, err := c.db.Exec(`UPDATE instagram_integrations SET owner_user_id=(SELECT id FROM users WHERE email='owner@example.com') WHERE instagram_user_id='inbox'`); err != nil {
		t.Fatal(err)
	}
	c.handler = Handler(c.db, Options{InstagramAccountID: "inbox", InstagramUsername: "akun_testing911", InstagramAccessToken: "token", InstagramWebhookVerifyToken: "verify", InstagramAppSecret: "app-secret"})
	payload := `{"object":"instagram","entry":[{"id":"inbox","messaging":[{"sender":{"id":"sender-1"},"recipient":{"id":"inbox"},"message":{"mid":"mid-unmatched","text":"instagram e2e"}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	var source, externalID, content string
	err := c.db.QueryRow(`SELECT source, external_message_id, content_markdown FROM notes`).Scan(&source, &externalID, &content)
	if w.Code != http.StatusOK || err != nil || source != "instagram" || externalID != "mid-unmatched" || content != "instagram e2e" {
		t.Fatalf("status=%d source=%q external=%q content=%q err=%v", w.Code, source, externalID, content, err)
	}
}

func TestInstagramWebhookSavesChangeTextToInboxOwner(t *testing.T) {
	_, c := webhookHandler(t, "verify", "app-secret")
	c.register(t, "Inbox Owner", "owner@example.com")
	if _, err := c.db.Exec(`UPDATE instagram_integrations SET owner_user_id=(SELECT id FROM users WHERE email='owner@example.com') WHERE instagram_user_id='inbox'`); err != nil {
		t.Fatal(err)
	}
	payload := `{"object":"instagram","entry":[{"id":"inbox","changes":[{"field":"messages","value":{"sender":{"id":"sender-change"},"recipient":{"id":"inbox"},"message":{"mid":"mid-change","text":"change payload"}}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	var source, externalID, content string
	err := c.db.QueryRow(`SELECT source, external_message_id, content_markdown FROM notes`).Scan(&source, &externalID, &content)
	if w.Code != http.StatusOK || err != nil || source != "instagram" || externalID != "mid-change" || content != "change payload" {
		t.Fatalf("status=%d source=%q external=%q content=%q err=%v", w.Code, source, externalID, content, err)
	}
}

func TestInstagramWebhookLogsSafeReceiptAndResults(t *testing.T) {
	h, _ := webhookHandler(t, "verify", "app-secret")
	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})

	for _, tc := range []struct {
		name, body, signature string
		wantStatus            int
		wantLogs              []string
	}{
		{"invalid signature", `invalid-signature-body`, "sha256=nope", http.StatusForbidden, []string{"instagram webhook receipt", "instagram webhook body=invalid-signature-body", "result=signature_invalid"}},
		{"malformed json", `{"private-message":`, signWebhook("app-secret", []byte(`{"private-message":`)), http.StatusBadRequest, []string{"instagram webhook receipt", "result=malformed_json"}},
		{"ignored echo", `{"object":"instagram","entry":[{"id":"raw-entry-id","messaging":[{"sender":{"id":"raw-sender-id"},"recipient":{"id":"raw-recipient-id"},"message":{"mid":"raw-mid","text":"private-message","is_echo":true}}]}]}`, "", http.StatusOK, []string{"instagram webhook receipt", "result=ok", "ignored=1", "echo=1"}},
		{"change metadata", `{"object":"instagram","entry":[{"id":"raw-entry-id","changes":[{"field":"messages","value":{"sender":{"id":"raw-sender-id"},"recipient":{"id":"raw-recipient-id"},"message":{"mid":"raw-mid","text":"private-message"}}}]}]}`, "", http.StatusOK, []string{"change field=messages", "event type=text", "result=ok object=instagram entries=1 changes=1 events=1 processed=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			signature := tc.signature
			if signature == "" {
				signature = signWebhook("app-secret", []byte(tc.body))
			}
			r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(tc.body))
			r.Header.Set("X-Hub-Signature-256", signature)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
			}
			got := logs.String()
			for _, want := range tc.wantLogs {
				if !strings.Contains(got, want) {
					t.Errorf("logs missing %q: %s", want, got)
				}
			}
			if tc.wantStatus == http.StatusOK {
				if !strings.Contains(got, "instagram webhook body=") || !strings.Contains(got, `"object":"instagram"`) || !strings.Contains(got, `"text":"private-message"`) {
					t.Errorf("logs missing full webhook body: %s", got)
				}
			}
			for _, secret := range []string{"signature-secret-message", "app-secret"} {
				if strings.Contains(got, secret) {
					t.Errorf("logs exposed %q: %s", secret, got)
				}
			}
		})
	}
}

func TestInstagramWebhookRoutesDedicatedRecipientAndUser2Sender(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := Handler(db, Options{InstagramAccountID: "17841426326903892", InstagramUsername: "akun_testing911", InstagramAccessToken: "token", InstagramUser2AccountID: "17841421563711996", InstagramWebhookVerifyToken: "verify", InstagramAppSecret: "app-secret"})
	c := &testClient{handler: h, db: db}
	c.register(t, "Inbox Owner", "owner@example.com")
	if _, err := c.db.Exec(`UPDATE instagram_integrations SET owner_user_id=(SELECT id FROM users WHERE email='owner@example.com') WHERE instagram_user_id='17841426326903892'`); err != nil {
		t.Fatal(err)
	}
	// Inbound DM: recipient = dedicated inbox, sender = user2. No usernames used.
	payload := `{"object":"instagram","entry":[{"id":"17841426326903892","messaging":[{"sender":{"id":"17841421563711996"},"recipient":{"id":"17841426326903892"},"message":{"mid":"mid-user2","text":"hello inbox"}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	var source, externalID, content, title string
	err = c.db.QueryRow(`SELECT source, external_message_id, content_markdown, title FROM notes`).Scan(&source, &externalID, &content, &title)
	if w.Code != http.StatusOK || err != nil || source != "instagram" || externalID != "mid-user2" || content != "hello inbox" || !strings.Contains(title, "17841421563711996") {
		t.Fatalf("status=%d source=%q external=%q content=%q title=%q err=%v", w.Code, source, externalID, content, title, err)
	}
}

func TestInstagramWebhookIgnoresEventsOutsideConfiguredInbox(t *testing.T) {
	_, c := webhookHandler(t, "verify", "app-secret")
	c.register(t, "Inbox Owner", "owner@example.com")
	if _, err := c.db.Exec(`UPDATE instagram_integrations SET owner_user_id=(SELECT id FROM users WHERE email='owner@example.com') WHERE instagram_user_id='inbox'`); err != nil {
		t.Fatal(err)
	}

	for _, payload := range []string{
		`{"object":"instagram","entry":[{"id":"other-account","messaging":[{"sender":{"id":"sender-1"},"recipient":{"id":"other-account"},"message":{"mid":"wrong-recipient","text":"ignore"}}]}]}`,
		`{"object":"page","entry":[{"id":"inbox","messaging":[{"sender":{"id":"sender-1"},"recipient":{"id":"inbox"},"message":{"mid":"wrong-object","text":"ignore"}}]}]}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
		r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
		w := httptest.NewRecorder()
		c.handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
		}
	}

	var notes int
	if err := c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil || notes != 0 {
		t.Fatalf("notes=%d err=%v", notes, err)
	}
}

func TestInstagramWebhookPassesWebhookRecipientToIntegrationLookup(t *testing.T) {
	_, c := webhookHandler(t, "verify", "app-secret")
	c.register(t, "Inbox Owner", "owner@example.com")
	if _, err := c.db.Exec(`UPDATE instagram_integrations SET instagram_user_id='17841426326903892', owner_user_id=(SELECT id FROM users WHERE email='owner@example.com')`); err != nil {
		t.Fatal(err)
	}
	payload := `{"object":"instagram","entry":[{"id":"entry-id-can-differ","messaging":[{"sender":{"id":"sender-1"},"recipient":{"id":"17841426326903892"},"message":{"mid":"real-recipient","text":"save me"}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	var externalID string
	if err := c.db.QueryRow(`SELECT external_message_id FROM notes`).Scan(&externalID); w.Code != http.StatusOK || err != nil || externalID != "real-recipient" {
		t.Fatalf("status=%d external=%q err=%v", w.Code, externalID, err)
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
