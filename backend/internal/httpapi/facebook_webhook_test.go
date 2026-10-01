package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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

func TestFacebookWebhookStoresOrderedFallbackAttachmentsWithText(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	body := `{"object":"page","entry":[{"id":"page-private-123","messaging":[{"sender":{"id":"sender-private-456"},"recipient":{"id":"page-private-123"},"message":{"mid":"text-with-media","text":"keep this text","attachments":[{"type":"fallback","payload":{"url":"https://www.facebook.com/share/p/first"}},{"type":"image","payload":{"url":"https://example.invalid/ignored"}},{"type":"fallback","payload":{"url":"https://www.facebook.com/share/p/second"}}]}}]}]}`
	w := facebookWebhookRequest(h, body, "app-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var content, attachments string
	if err := db.QueryRow(`SELECT content_markdown, facebook_attachments_json FROM notes WHERE external_message_id='text-with-media'`).Scan(&content, &attachments); err != nil {
		t.Fatal(err)
	}
	if content != "keep this text" || attachments != `[{"type":"fallback","url":"https://www.facebook.com/share/p/first","lookup_status":"unavailable"},{"type":"fallback","url":"https://www.facebook.com/share/p/second","lookup_status":"unavailable"}]` {
		t.Fatalf("note content=%q attachments=%s", content, attachments)
	}
}

func TestFacebookWebhookCreatesAttachmentOnlyNoteAndReturnsItFromAPI(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	c := newTestClientWithHandler(t, h)
	c.register(t, "Owner", "facebook-attachment-owner@example.com")
	if _, err := db.Exec(`UPDATE social_identities SET user_id=? WHERE platform='facebook' AND platform_user_id='sender-private-456'`, 2); err != nil {
		t.Fatal(err)
	}
	body := `{"object":"page","entry":[{"id":"page-private-123","messaging":[{"sender":{"id":"sender-private-456"},"recipient":{"id":"page-private-123"},"message":{"mid":"attachment-only","attachments":[{"type":"fallback","payload":{"url":"https://www.facebook.com/share/p/only"}}]}}]}]}`
	for range 2 {
		if w := facebookWebhookRequest(h, body, "app-secret"); w.Code != http.StatusOK {
			t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
		}
	}
	var noteID int
	var count int
	if err := db.QueryRow(`SELECT count(*), id FROM notes WHERE external_message_id='attachment-only'`).Scan(&count, &noteID); err != nil || count != 1 {
		t.Fatalf("note count=%d id=%d err=%v", count, noteID, err)
	}
	res := c.request(t, http.MethodGet, "/api/notes/"+strconv.Itoa(noteID), nil, false)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"facebook_attachments":[{"type":"fallback","url":"https://www.facebook.com/share/p/only","lookup_status":"unavailable"}]`) || !strings.Contains(res.Body.String(), `"instagram_attachments":[]`) {
		t.Fatalf("detail=%d %s", res.Code, res.Body.String())
	}
}

func TestFacebookWebhookIgnoresInvalidAttachmentsAndDoesNotStoreMediaForUntrustedMessages(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	for _, body := range []string{
		`{"object":"page","entry":[{"id":"page-private-123","messaging":[{"sender":{"id":"sender-private-456"},"recipient":{"id":"page-private-123"},"message":{"mid":"invalid-only","attachments":[{"type":"fallback","payload":{"url":"http://www.facebook.com/not-https"}},{"type":"fallback","payload":{}},"malformed"]}}]}]}`,
		`{"object":"page","entry":[{"id":"page-private-123","messaging":[{"sender":{"id":"unknown"},"recipient":{"id":"page-private-123"},"message":{"mid":"unknown-media","text":"must not save","attachments":[{"type":"fallback","payload":{"url":"https://www.facebook.com/share/p/unknown"}}]}}]}]}`,
		`{"object":"page","entry":[{"id":"wrong-page","messaging":[{"sender":{"id":"sender-private-456"},"recipient":{"id":"wrong-page"},"message":{"mid":"wrong-page-media","attachments":[{"type":"fallback","payload":{"url":"https://www.facebook.com/share/p/wrong"}}]}}]}]}`,
	} {
		if w := facebookWebhookRequest(h, body, "app-secret"); w.Code != http.StatusOK {
			t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
		}
	}
	var notes, storedMedia int
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM notes WHERE facebook_attachments_json IS NOT NULL`).Scan(&storedMedia); err != nil {
		t.Fatal(err)
	}
	if notes != 0 || storedMedia != 0 {
		t.Fatalf("notes=%d stored media=%d", notes, storedMedia)
	}
}

func TestFacebookWebhookKeepsTextWhenAllAttachmentsAreInvalid(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	body := `{"object":"page","entry":[{"id":"page-private-123","messaging":[{"sender":{"id":"sender-private-456"},"recipient":{"id":"page-private-123"},"message":{"mid":"text-invalid-media","text":"keep the text","attachments":[{"type":"image","payload":{"url":"https://example.invalid/image"}},{"type":"fallback","payload":{"url":"http://www.facebook.com/not-https"}}]}}]}]}`
	if w := facebookWebhookRequest(h, body, "app-secret"); w.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var content string
	var attachments sql.NullString
	if err := db.QueryRow(`SELECT content_markdown, facebook_attachments_json FROM notes WHERE external_message_id='text-invalid-media'`).Scan(&content, &attachments); err != nil {
		t.Fatal(err)
	}
	if content != "keep the text" || attachments.Valid {
		t.Fatalf("content=%q attachments=%v", content, attachments)
	}
}

func TestFacebookNoteCanBeViewedAndUpdatedByItsOwner(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	c := newTestClientWithHandler(t, h)
	c.register(t, "Owner", "facebook-note-owner@example.com")
	if _, err := db.Exec(`UPDATE social_identities SET user_id=? WHERE platform='facebook' AND platform_user_id='sender-private-456'`, 2); err != nil {
		t.Fatal(err)
	}
	if w := facebookWebhookRequest(h, facebookPayload("page-private-123", "sender-private-456", "editable-facebook-note", "original Facebook text", false, false), "app-secret"); w.Code != http.StatusOK {
		t.Fatalf("webhook=%d %s", w.Code, w.Body.String())
	}
	var noteID int
	if err := db.QueryRow(`SELECT id FROM notes WHERE external_message_id='editable-facebook-note'`).Scan(&noteID); err != nil {
		t.Fatal(err)
	}
	path := "/api/notes/" + strconv.Itoa(noteID)
	view := c.request(t, http.MethodGet, path, nil, false)
	if view.Code != http.StatusOK || !strings.Contains(view.Body.String(), `"source":"facebook"`) || !strings.Contains(view.Body.String(), `"content_markdown":"original Facebook text"`) {
		t.Fatalf("detail=%d %s", view.Code, view.Body.String())
	}
	updated := c.request(t, http.MethodPut, path, map[string]string{"title": "Updated Facebook note", "content_markdown": "edited Facebook text"}, true)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"title":"Updated Facebook note"`) || !strings.Contains(updated.Body.String(), `"source":"facebook"`) {
		t.Fatalf("update=%d %s", updated.Code, updated.Body.String())
	}
	reloaded := c.request(t, http.MethodGet, path, nil, false)
	if reloaded.Code != http.StatusOK || !strings.Contains(reloaded.Body.String(), `"content_markdown":"edited Facebook text"`) || !strings.Contains(reloaded.Body.String(), `"source":"facebook"`) {
		t.Fatalf("reloaded detail=%d %s", reloaded.Code, reloaded.Body.String())
	}
}

func TestFacebookMessagesAPIListsOwnedReceiptsAndRequiresAuthentication(t *testing.T) {
	h, db := facebookWebhookHandler(t)
	if w := facebookWebhookRequest(h, facebookPayload("page-private-123", "sender-private-456", "message-list-1", "listed message", false, false), "app-secret"); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	anonymous := httptest.NewRecorder()
	h.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/facebook-messages", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", anonymous.Code)
	}
	c := newTestClientWithHandler(t, h)
	c.register(t, "Owner", "owner-list@example.com")
	if _, err := db.Exec(`UPDATE social_identities SET user_id=? WHERE platform='facebook' AND platform_user_id='sender-private-456'`, 2); err != nil {
		t.Fatal(err)
	}
	res := c.request(t, http.MethodGet, "/api/facebook-messages", nil, false)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"text":"listed message"`) || !strings.Contains(res.Body.String(), `"status":"note_created"`) {
		t.Fatalf("list = %d %s", res.Code, res.Body.String())
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
	var ignored int
	if err := db.QueryRow(`SELECT count(*) FROM facebook_message_receipts WHERE status='ignored' AND message_text IN ('echo text', 'wrong page', '')`).Scan(&ignored); err != nil || ignored != 3 {
		t.Fatalf("ignored receipts=%d err=%v", ignored, err)
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
	for _, secret := range []string{"page-private-123", "sender-private-456", "message-private-789", "message body must stay private", "https://example.invalid/private", "app-secret", "sha256="} {
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

func TestFacebookReelAttachmentObservedContractAndURLSafety(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"type":"unknown-provider-type","payload":{"reel_video_id":"redacted","title":"redacted","url":"https://www.facebook.com/reel/redacted"}}`),
		json.RawMessage(`{"type":"fallback","payload":{"url":"https://evil.example/post"}}`),
		json.RawMessage(`{"type":"fallback","payload":{"url":"https://user@www.facebook.com/post"}}`),
		json.RawMessage(`{"type":"fallback","payload":{"url":"https://www.facebook.com/post#fragment"}}`),
		json.RawMessage(`{"type":"fallback","payload":{"url":"//www.facebook.com/post"}}`),
	}
	want := []store.FacebookAttachment{{Type: "reel", URL: "https://www.facebook.com/reel/redacted", Title: "redacted", LookupStatus: "captured"}}
	if got := supportedFacebookAttachments(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("attachments=%#v want=%#v", got, want)
	}
}
func TestSupportedFacebookAttachmentsLocksObservedFallbackContract(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"type":"fallback","payload":{"url":"https://www.facebook.com/share/p/first","title":"A useful shared post"}}`),
		json.RawMessage(`{"type":"image","payload":{"url":"https://example.invalid/image"}}`),
		json.RawMessage(`{"type":"fallback","payload":{"url":"http://www.facebook.com/not-https"}}`),
		json.RawMessage(`{"type":"fallback","payload":{"url":"https:///missing-host"}}`),
		json.RawMessage(`{"type":"fallback","payload":{"url":"https://www.facebook.com/share/p/second"}}`),
		json.RawMessage(`"malformed"`),
	}
	got := supportedFacebookAttachments(items)
	want := []store.FacebookAttachment{
		{Type: "fallback", URL: "https://www.facebook.com/share/p/first", Title: "A useful shared post", LookupStatus: "captured"},
		{Type: "fallback", URL: "https://www.facebook.com/share/p/second", LookupStatus: "unavailable"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attachments=%#v want=%#v", got, want)
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
