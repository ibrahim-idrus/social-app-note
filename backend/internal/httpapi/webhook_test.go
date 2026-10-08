package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"social-notes/backend/internal/store"
)

func TestInstagramMessageLookupRequestsCompleteFields(t *testing.T) {
	var request *http.Request
	api := API{
		instagramAccessToken:  "test-token",
		instagramGraphVersion: "v26.0",
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			request = req
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		})},
	}

	if _, err := api.lookupInstagramMessage(context.Background(), "message-mid"); err != nil {
		t.Fatal(err)
	}
	if request.Method != http.MethodGet || request.URL.Path != "/v26.0/message-mid" {
		t.Fatalf("request = %s %s", request.Method, request.URL.Path)
	}
	if got, want := request.URL.RawQuery, "fields=message%2Cattachments%2Cshares%7Bdata%7Bname%2Cdescription%2Ctype%2Curl%2Cid%7D%7D"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("authorization = %q", got)
	}

	requests := 0
	api.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected request")
	})}
	api.instagramAccessToken = ""
	if _, err := api.lookupInstagramMessage(context.Background(), "message-mid"); err != nil {
		t.Fatal(err)
	}
	api.instagramAccessToken = "test-token"
	if _, err := api.lookupInstagramMessage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("empty token or MID made %d requests", requests)
	}
}

func webhookHandler(t *testing.T, verifyToken, appSecret string) (http.Handler, *testClient) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := Handler(db, Options{InstagramAccountID: "inbox", InstagramUsername: "akun_testing911", InstagramAccessToken: "token", InstagramWebhookVerifyToken: verifyToken, InstagramAppSecret: appSecret, HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}})
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

func TestInstagramWebhookIgnoresUnmatchedSender(t *testing.T) {
	_, c := webhookHandler(t, "verify", "app-secret")
	c.register(t, "Inbox Owner", "owner@example.com")
	if _, err := c.db.Exec(`UPDATE instagram_integrations SET owner_user_id=(SELECT id FROM users WHERE email='owner@example.com') WHERE instagram_user_id='inbox'`); err != nil {
		t.Fatal(err)
	}
	c.handler = Handler(c.db, Options{InstagramAccountID: "inbox", InstagramUsername: "akun_testing911", InstagramAccessToken: "token", InstagramWebhookVerifyToken: "verify", InstagramAppSecret: "app-secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}})
	payload := `{"object":"instagram","entry":[{"id":"inbox","messaging":[{"sender":{"id":"sender-1"},"recipient":{"id":"inbox"},"message":{"mid":"mid-unmatched","text":"instagram e2e"}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	var notes, identities int
	_ = c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes)
	_ = c.db.QueryRow(`SELECT count(*) FROM social_identities`).Scan(&identities)
	if w.Code != http.StatusOK || notes != 0 || identities != 0 {
		t.Fatalf("status=%d notes=%d identities=%d", w.Code, notes, identities)
	}
}

func TestInstagramWebhookIgnoresUnmatchedChangeText(t *testing.T) {
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
	var notes int
	_ = c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes)
	if w.Code != http.StatusOK || notes != 0 {
		t.Fatalf("status=%d notes=%d", w.Code, notes)
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
		{"invalid signature", `invalid-signature-body`, "sha256=nope", http.StatusForbidden, []string{"instagram webhook receipt", "result=signature_invalid"}},
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
			got = strings.Replace(got, "instagram webhook raw body="+tc.body+"\n", "", 1)
			for _, secret := range []string{"private-message", "invalid-signature-body", "raw-sender-id", "raw-recipient-id", "raw-mid", "signature-secret-message", "app-secret"} {
				if strings.Contains(got, secret) {
					t.Errorf("logs exposed %q: %s", secret, got)
				}
			}
		})
	}
}

func TestInstagramWebhookRecordsSignedRawBodyBeforeParsing(t *testing.T) {
	h, _ := webhookHandler(t, "verify", "app-secret")
	oldWriter, oldFlags := log.Writer(), log.Flags()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(oldWriter); log.SetFlags(oldFlags) })

	body := `{"raw":"before-parser"`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(body)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	got := logs.String()
	resultAt := strings.Index(got, "instagram webhook result=malformed_json")
	if w.Code != http.StatusBadRequest || resultAt < 0 || strings.Contains(got, body) {
		t.Fatalf("status=%d result_at=%d logs=%q", w.Code, resultAt, got)
	}

	logs.Reset()
	r = httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", signWebhook("wrong-secret", []byte(body)))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if strings.Contains(logs.String(), body) {
		t.Fatalf("unauthenticated body recorded: %q", logs.String())
	}
}

func TestInstagramWebhookUnsupportedAttachmentDiagnosticIsSafe(t *testing.T) {
	h, _ := webhookHandler(t, "verify", "app-secret")
	oldWriter, oldFlags := log.Writer(), log.Flags()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(oldWriter); log.SetFlags(oldFlags) })

	body := `{"object":"instagram","entry":[{"id":"raw-entry-id","messaging":[{"sender":{"id":"raw-sender-id"},"recipient":{"id":"raw-recipient-id"},"message":{"mid":"raw-mid","attachments":[{"type":"private-type","payload":{"title":"private-title","url":"https://private.example/path?token=private-token","thread_post_id":"private-media-id","generic":{"elements":[{"action_url":"https://private.example/thread","image_url":"https://private.example/image","title":"private-card-title"}]}}}]}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(body)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	got := strings.Replace(logs.String(), "instagram webhook raw body="+body+"\n", "", 1)
	for _, want := range []string{"unsupported attachment index=0", "type=private-type", "payload_keys=generic,thread_post_id,title,url", "ig_media_id=false", "reel_video_id=false", "url=true", "url_parse_ok=true", "scheme=https", "host=other", "generic_shape={elements:array(1,{action_url:nested,image_url:nested,title:nested})}", "missing_text=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("logs missing %q: %s", want, got)
		}
	}
	for _, private := range []string{"private-title", "private-card-title", "private.example", "/path", "/thread", "/image", "private-token", "private-media-id", "raw-entry-id", "raw-sender-id", "raw-recipient-id", "raw-mid", "app-secret"} {
		if strings.Contains(got, private) {
			t.Errorf("logs exposed %q: %s", private, got)
		}
	}
}

func TestInstagramWebhookDoesNotTrustConfiguredUser2WithoutVerification(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := Handler(db, Options{InstagramAccountID: "17841426326903892", InstagramUsername: "akun_testing911", InstagramAccessToken: "token", InstagramUser2AccountID: "17841421563711996", InstagramWebhookVerifyToken: "verify", InstagramAppSecret: "app-secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}})
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
	var notes int
	err = c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes)
	if w.Code != http.StatusOK || err != nil || notes != 0 {
		t.Fatalf("status=%d notes=%d err=%v", w.Code, notes, err)
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

func TestInstagramWebhookRecipientMatchDoesNotBypassSenderVerification(t *testing.T) {
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
	var notes int
	if err := c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); w.Code != http.StatusOK || err != nil || notes != 0 {
		t.Fatalf("status=%d notes=%d err=%v", w.Code, notes, err)
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

func TestInstagramWebhookProcessingSurvivesMessageLookupFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lookupCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/me") {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"ig-alice","username":"alice"}`)), Header: make(http.Header)}, nil
		}
		if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/mid-lookup-failure") {
			lookupCalls++
			return nil, errors.New("provider failure containing instagram-user-token and https://graph.instagram.com/private")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	h := Handler(db, Options{InstagramAccountID: "inbox", InstagramUsername: "akun_testing911", InstagramAccessToken: "instagram-user-token", InstagramWebhookVerifyToken: "verify", InstagramAppSecret: "app-secret", HTTPClient: client})
	c := &testClient{handler: h, db: db}
	c.register(t, "Alice", "alice@example.com")
	registration := registerInstagram(t, c, "alice")

	oldWriter, oldFlags := log.Writer(), log.Flags()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(oldWriter); log.SetFlags(oldFlags) })
	payload := `{"object":"instagram","entry":[{"id":"inbox","messaging":[{"sender":{"id":"sender-1"},"recipient":{"id":"inbox"},"message":{"mid":"mid-lookup-failure","text":"` + registration.VerificationCode + `"}}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/integrations/instagram/webhook", strings.NewReader(payload))
	r.Header.Set("X-Hub-Signature-256", signWebhook("app-secret", []byte(payload)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	var status, platformID string
	err = db.QueryRow(`SELECT status, platform_user_id FROM social_identities WHERE id=?`, registration.ID).Scan(&status, &platformID)
	if w.Code != http.StatusOK || lookupCalls != 0 || err != nil || status != "active" || platformID != "sender-1" {
		t.Fatalf("status_code=%d lookups=%d identity_status=%q platform_id=%q err=%v", w.Code, lookupCalls, status, platformID, err)
	}
	got := logs.String()
	for _, secret := range []string{"instagram-user-token", "https://graph.instagram.com/private"} {
		if strings.Contains(got, secret) {
			t.Fatalf("webhook log exposed %q: %s", secret, got)
		}
	}
}

func TestInstagramLinksExtractsSafePostAndReelPermalinks(t *testing.T) {
	attachments := instagramLinks(json.RawMessage(`{"attachments":{"data":[{"url":"https://www.instagram.com/p/POST123/"},{"url":"https://www.instagram.com/reel/REEL123/"},{"url":"https://evil.example/p/NOPE/"},{"url":"https://www.instagram.com/p/POST123/?token=secret"}]}}`))
	if len(attachments) != 2 || attachments[0].Type != "ig_post" || attachments[0].Permalink != "https://www.instagram.com/p/POST123/" || attachments[1].Type != "ig_reel" || attachments[1].Permalink != "https://www.instagram.com/reel/REEL123/" {
		t.Fatalf("attachments=%#v", attachments)
	}
}

func TestObservedInstagramPostFixtureContract(t *testing.T) {
	body, err := os.ReadFile("testdata/instagram-post.json")
	if err != nil {
		t.Fatal(err)
	}
	var payload instagramWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	attachments := supportedInstagramAttachments(payload.Entry[0].Messaging[0].Message.Attachments)
	if len(attachments) != 1 || attachments[0].Type != "ig_post" || attachments[0].URL == "" || attachments[0].InstagramMediaID != "MEDIA_ID" || attachments[0].Permalink != "https://www.instagram.com/p/REDACTED/" {
		t.Fatalf("attachments=%#v", attachments)
	}
}

func TestObservedInstagramReelFixtureContract(t *testing.T) {
	body, err := os.ReadFile("testdata/instagram-reel.json")
	if err != nil {
		t.Fatal(err)
	}
	var payload instagramWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	attachments := supportedInstagramAttachments(payload.Entry[0].Messaging[0].Message.Attachments)
	if len(attachments) != 1 || attachments[0].Type != "ig_reel" || attachments[0].URL != "https://www.instagram.com/reel/REDACTED/" || attachments[0].InstagramMediaID != "REEL_MEDIA_ID" || attachments[0].Permalink != attachments[0].URL {
		t.Fatalf("attachments=%#v", attachments)
	}
}
