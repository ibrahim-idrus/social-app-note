package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"social-notes/backend/internal/store"
)

func TestFacebookWebhookVerification(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	h := Handler(db, Options{FacebookWebhookVerifyToken: "facebook-secret"})
	for _, tc := range []struct {
		name, query string
		want        int
		body        string
	}{
		{"valid", "hub.mode=subscribe&hub.verify_token=facebook-secret&hub.challenge=TEST123", http.StatusOK, "TEST123"},
		{"bad token", "hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=TEST123", http.StatusForbidden, ""},
		{"bad mode", "hub.mode=wrong&hub.verify_token=facebook-secret&hub.challenge=TEST123", http.StatusForbidden, ""},
		{"missing challenge", "hub.mode=subscribe&hub.verify_token=facebook-secret", http.StatusForbidden, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/integrations/facebook/webhook?"+tc.query, nil)
			h.ServeHTTP(w, r)
			if w.Code != tc.want || w.Body.String() != tc.body {
				t.Fatalf("got status=%d body=%q, want status=%d body=%q", w.Code, w.Body.String(), tc.want, tc.body)
			}
			if strings.Contains(w.Body.String(), "facebook-secret") {
				t.Fatal("response exposed verify token")
			}
			if tc.want == http.StatusOK && w.Header().Get("Content-Type") != "text/plain" {
				t.Fatalf("Content-Type = %q, want text/plain", w.Header().Get("Content-Type"))
			}
		})
	}
}

func TestFacebookWebhookPostRouteRegistered(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	body := `{"object":"page","entry":[]}`
	w := facebookWebhookRequest(Handler(db, Options{FacebookAppSecret: "app-secret", FacebookPageID: "page-route"}), body, "app-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestFacebookWebhookProcessesSignedMessengerMessage(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	body := facebookPayload("page-private-123", "sender-private-456", "message-private-789", "private note text", false, false)
	w := facebookWebhookRequest(h, body, "app-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var content, source string
	if err := db.QueryRow(`SELECT content_markdown, source FROM notes WHERE external_message_id='message-private-789'`).Scan(&content, &source); err != nil {
		t.Fatal(err)
	}
	if content != "private note text" || source != "facebook" {
		t.Fatalf("note content=%q source=%q", content, source)
	}
}

func TestFacebookWebhookRejectsInvalidSignature(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	w := facebookWebhookRequest(h, facebookPayload("page-private-123", "sender-private-456", "message-private-789", "must not save", false, false), "wrong-secret")
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("notes=%d err=%v", count, err)
	}
}

func TestFacebookWebhookIgnoresUnsupportedEvents(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	for _, body := range []string{
		facebookPayload("page-private-123", "sender-private-456", "echo-id", "echo text", true, false),
		facebookPayload("page-private-123", "sender-private-456", "attachment-id", "", false, true),
		facebookPayload("wrong-page", "sender-private-456", "wrong-page-id", "wrong page", false, false),
		`{"object":"instagram","entry":[]}`,
	} {
		if w := facebookWebhookRequest(h, body, "app-secret"); w.Code != http.StatusOK {
			t.Fatalf("ignored POST status=%d body=%s", w.Code, w.Body.String())
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("notes=%d err=%v", count, err)
	}
}

func TestFacebookWebhookDuplicateAndPrivacySafeLogs(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	var logs bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	body := facebookPayload("page-private-123", "sender-private-456", "message-private-789", "message body must stay private", false, false)
	for range 2 {
		if w := facebookWebhookRequest(h, body, "app-secret"); w.Code != http.StatusOK {
			t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if w := facebookWebhookRequest(h, facebookPayload("page-private-123", "sender-private-456", "attachment-private-000", "", false, true), "app-secret"); w.Code != http.StatusOK {
		t.Fatalf("ignored POST status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM notes WHERE external_message_id='message-private-789'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("notes=%d err=%v", count, err)
	}
	text := logs.String()
	for _, secret := range []string{"page-private-123", "sender-private-456", "message-private-789", "message body must stay private", "app-secret", "sha256="} {
		if strings.Contains(text, secret) {
			t.Fatalf("logs exposed %q: %s", secret, text)
		}
	}
	for _, required := range []string{webhookID("sender-private-456"), webhookID("page-private-123"), webhookID("message-private-789"), "processing_ran=true processing_result=note_created", "processing_ran=true processing_result=duplicate", "processing_ran=false processing_result=missing_text"} {
		if !strings.Contains(text, required) {
			t.Fatalf("logs missing %q: %s", required, text)
		}
	}
}

func facebookWebhookHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`INSERT INTO users (id, name, email, password_hash) VALUES (1, 'Alice', 'alice@example.com', 'hash')`,
		`INSERT INTO social_identities (id, user_id, platform, platform_user_id, username, normalized_username, status) VALUES (1, 1, 'facebook', 'sender-private-456', '', '', 'active')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return Handler(db, Options{FacebookAppSecret: "app-secret", FacebookPageID: "page-private-123"}), db
}

func facebookWebhookRequest(h http.Handler, body, secret string) *httptest.ResponseRecorder {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/facebook/webhook", strings.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func facebookPayload(pageID, senderID, messageID, text string, echo, attachment bool) string {
	attachments := ""
	if attachment {
		attachments = `,"attachments":[{"type":"image","payload":{"url":"https://example.invalid/private"}}]`
	}
	return `{"object":"page","entry":[{"id":"` + pageID + `","messaging":[{"sender":{"id":"` + senderID + `"},"recipient":{"id":"` + pageID + `"},"message":{"mid":"` + messageID + `","text":"` + text + `","is_echo":` + strconv.FormatBool(echo) + attachments + `}}]}]}`
}
