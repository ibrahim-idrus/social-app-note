package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"social-notes/backend/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newInstagramReplyTestClient(t *testing.T, transport roundTripFunc) *testClient {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &testClient{
		handler: Handler(db, Options{
			InstagramAccountID:   "17841426326903892",
			InstagramUsername:    "notedesk_inbox",
			InstagramAccessToken: "instagram-user-token",
			HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodGet && req.URL.Host == "graph.instagram.com" && strings.HasSuffix(req.URL.Path, "/me") {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"ig-alice","username":"alice"}`)), Header: make(http.Header)}, nil
				}
				return transport(req)
			})},
		}),
		db: db,
	}
}

type instagramRegistration struct {
	ID                    int64   `json:"id"`
	Username              string  `json:"username"`
	Status                string  `json:"status"`
	VerificationState     string  `json:"verification_state"`
	VerificationUpdatedAt *string `json:"verification_updated_at"`
	VerificationCode      string  `json:"verification_code"`
	VerificationExpiresAt string  `json:"verification_expires_at"`
	InstagramAccount      struct {
		InstagramUserID string `json:"instagram_user_id"`
		Username        string `json:"username"`
	} `json:"instagram_account"`
}

func registerInstagram(t *testing.T, c *testClient, username string) instagramRegistration {
	t.Helper()
	res := c.request(t, http.MethodPost, "/api/social-identities", map[string]string{
		"platform": "instagram", "username": username,
	}, true)
	if res.Code != http.StatusCreated {
		t.Fatalf("create identity = %d %s", res.Code, res.Body.String())
	}
	var registration instagramRegistration
	if err := json.Unmarshal(res.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	return registration
}

func simulatedDM(t *testing.T, c *testClient, recipient, sender, externalID, text string, extra map[string]any) *bytes.Buffer {
	t.Helper()
	body := map[string]any{
		"recipient_instagram_user_id": recipient,
		"sender_platform_user_id":     sender,
		"external_message_id":         externalID,
		"text":                        text,
	}
	for key, value := range extra {
		body[key] = value
	}
	res := c.request(t, http.MethodPost, "/api/integrations/instagram/simulated-dm", body, false)
	if res.Code != http.StatusAccepted || res.Body.String() != "{\"status\":\"accepted\"}\n" {
		t.Fatalf("simulated DM = %d %s", res.Code, res.Body.String())
	}
	return res.Body
}

func TestInstagramRegistrationReturnsVisibleCodeButStoresOnlyHash(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")
	registration := registerInstagram(t, c, "Alice")

	if registration.Status != "pending" || registration.VerificationCode == "" || registration.InstagramAccount.InstagramUserID == "" || registration.InstagramAccount.Username == "" {
		t.Fatalf("registration = %#v", registration)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, registration.VerificationExpiresAt)
	if err != nil || time.Until(expiresAt) < 9*time.Minute || time.Until(expiresAt) > 10*time.Minute+time.Second {
		t.Fatalf("expiry = %q, err %v", registration.VerificationExpiresAt, err)
	}
	var hash []byte
	var expires string
	if err := c.db.QueryRow(`SELECT verification_code_hash, verification_expires_at FROM social_identities WHERE id = ?`, registration.ID).Scan(&hash, &expires); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(registration.VerificationCode))
	if bytes.Equal(hash, []byte(registration.VerificationCode)) || !bytes.Equal(hash, wantHash[:]) || expires != registration.VerificationExpiresAt {
		t.Fatalf("stored verification = hash %x expiry %q", hash, expires)
	}
	listed := c.request(t, http.MethodGet, "/api/social-identities", nil, false)
	if strings.Contains(listed.Body.String(), registration.VerificationCode) || strings.Contains(listed.Body.String(), "verification_code_hash") {
		t.Fatalf("list leaked verification secret: %s", listed.Body.String())
	}
}

func TestInstagramCodeReplacementExpirySingleUseAndActivation(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")
	first := registerInstagram(t, c, "alice")

	replaced := c.request(t, http.MethodPost, "/api/social-identities/"+itoa(int(first.ID))+"/verification-code", nil, true)
	if replaced.Code != http.StatusOK {
		t.Fatalf("regenerate = %d %s", replaced.Code, replaced.Body.String())
	}
	var second instagramRegistration
	if err := json.Unmarshal(replaced.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.VerificationCode == "" || second.VerificationCode == first.VerificationCode {
		t.Fatalf("replacement code = %q after %q", second.VerificationCode, first.VerificationCode)
	}

	simulatedDM(t, c, second.InstagramAccount.InstagramUserID, "sender-1", "verify-old", first.VerificationCode, nil)
	var status string
	if err := c.db.QueryRow(`SELECT status FROM social_identities WHERE id = ?`, first.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("old code activated identity: %s", status)
	}

	if _, err := c.db.Exec(`UPDATE social_identities SET verification_expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), first.ID); err != nil {
		t.Fatal(err)
	}
	simulatedDM(t, c, second.InstagramAccount.InstagramUserID, "sender-1", "verify-expired", second.VerificationCode, nil)
	if err := c.db.QueryRow(`SELECT status FROM social_identities WHERE id = ?`, first.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("expired code activated identity: %s", status)
	}

	thirdResponse := c.request(t, http.MethodPost, "/api/social-identities/"+itoa(int(first.ID))+"/verification-code", nil, true)
	var third instagramRegistration
	if err := json.Unmarshal(thirdResponse.Body.Bytes(), &third); err != nil {
		t.Fatal(err)
	}
	simulatedDM(t, c, third.InstagramAccount.InstagramUserID, "sender-1", "verify-good", third.VerificationCode, nil)
	var platformUserID, verifiedAt string
	var consumedAt any
	if err := c.db.QueryRow(`SELECT status, platform_user_id, verified_at, verification_consumed_at FROM social_identities WHERE id = ?`, first.ID).Scan(&status, &platformUserID, &verifiedAt, &consumedAt); err != nil {
		t.Fatal(err)
	}
	if status != "active" || platformUserID != "sender-1" || verifiedAt == "" || consumedAt == nil {
		t.Fatalf("activated identity = %q %q %q %#v", status, platformUserID, verifiedAt, consumedAt)
	}
	var noteCount int
	if err := c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&noteCount); err != nil || noteCount != 0 {
		t.Fatalf("verification note count = %d, err %v", noteCount, err)
	}
	simulatedDM(t, c, third.InstagramAccount.InstagramUserID, "sender-1", "verify-reused", third.VerificationCode, nil)
	if err := c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&noteCount); err != nil || noteCount != 0 {
		t.Fatalf("reused code note count = %d, err %v", noteCount, err)
	}
}

func TestInstagramStableIDRoutingIsolationIdempotencyAndSafeIgnores(t *testing.T) {
	a := newTestClient(t, false)
	a.register(t, "Alice", "alice@example.com")
	alice := registerInstagram(t, a, "alice")
	simulatedDM(t, a, alice.InstagramAccount.InstagramUserID, "stable-alice", "verify-a", alice.VerificationCode, nil)

	b := newTestClientWithHandler(t, Handler(a.db, Options{
		InstagramAccountID: "selected-account", InstagramUsername: "notedesk_inbox", InstagramAccessToken: "instagram-user-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/me") {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"ig-bob","username":"bob"}`)), Header: make(http.Header)}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		})},
	}))
	b.db = a.db
	b.register(t, "Bob", "bob@example.com")
	bob := registerInstagram(t, b, "bob")
	simulatedDM(t, a, bob.InstagramAccount.InstagramUserID, "stable-bob", "verify-b", bob.VerificationCode, nil)

	simulatedDM(t, a, alice.InstagramAccount.InstagramUserID, "stable-alice", "message-1", "Alice private note", nil)
	simulatedDM(t, a, alice.InstagramAccount.InstagramUserID, "stable-alice", "message-1", "retry must not duplicate", nil)
	simulatedDM(t, a, alice.InstagramAccount.InstagramUserID, "stable-bob", "message-2", "Bob private note", nil)
	for _, ignored := range []struct {
		recipient, sender, id, text string
		extra                       map[string]any
	}{
		{"wrong-recipient", "stable-alice", "ignored-wrong-recipient", "ignore", nil},
		{alice.InstagramAccount.InstagramUserID, "unknown", "ignored-unknown", "@alice does not route", nil},
		{alice.InstagramAccount.InstagramUserID, "stable-alice", "ignored-self", "ignore", map[string]any{"is_self": true}},
		{alice.InstagramAccount.InstagramUserID, "stable-alice", "ignored-unsupported", "ignore", map[string]any{"message_type": "image"}},
	} {
		simulatedDM(t, a, ignored.recipient, ignored.sender, ignored.id, ignored.text, ignored.extra)
	}

	var aliceUserID, bobUserID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE email = 'alice@example.com'`).Scan(&aliceUserID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT id FROM users WHERE email = 'bob@example.com'`).Scan(&bobUserID); err != nil {
		t.Fatal(err)
	}
	var aliceNotes, bobNotes int
	if err := a.db.QueryRow(`SELECT count(*) FROM notes WHERE user_id = ? AND content_markdown = 'Alice private note'`, aliceUserID).Scan(&aliceNotes); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT count(*) FROM notes WHERE user_id = ? AND content_markdown = 'Bob private note'`, bobUserID).Scan(&bobNotes); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := a.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if aliceNotes != 1 || bobNotes != 1 || total != 2 {
		t.Fatalf("notes: alice=%d bob=%d total=%d", aliceNotes, bobNotes, total)
	}
}

func TestInstagramVerificationSendsOneSanitizedSuccessReply(t *testing.T) {
	var requests []*http.Request
	var bodies [][]byte
	c := newInstagramReplyTestClient(t, func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, req)
		bodies = append(bodies, body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"recipient_id":"sender-1","message_id":"provider-secret"}`)),
			Header:     make(http.Header),
		}, nil
	})
	c.register(t, "Alice", "alice@example.com")
	registration := registerInstagram(t, c, "alice")

	simulatedDM(t, c, registration.InstagramAccount.InstagramUserID, "sender-1", "verify-good", registration.VerificationCode, nil)
	simulatedDM(t, c, registration.InstagramAccount.InstagramUserID, "sender-1", "verify-good", registration.VerificationCode, nil)

	if len(requests) != 1 {
		t.Fatalf("reply requests = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.Method != http.MethodPost || request.URL.String() != "https://graph.instagram.com/v26.0/17841426326903892/messages" {
		t.Fatalf("reply request = %s %s", request.Method, request.URL)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer instagram-user-token" {
		t.Fatalf("authorization = %q", got)
	}
	if got := request.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	var payload struct {
		Recipient struct {
			ID string `json:"id"`
		} `json:"recipient"`
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
	}
	if err := json.Unmarshal(bodies[0], &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Recipient.ID != "sender-1" || !strings.Contains(payload.Message.Text, "NoteDesk") || !strings.Contains(payload.Message.Text, "@alice") || strings.Contains(payload.Message.Text, "alice@example.com") {
		t.Fatalf("reply payload = %#v", payload)
	}
	var notes int
	if err := c.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil || notes != 0 {
		t.Fatalf("verification note count = %d, err %v", notes, err)
	}
	listed := c.request(t, http.MethodGet, "/api/social-identities", nil, false).Body.String()
	for _, secret := range []string{"instagram-user-token", "provider-secret", "verify-good"} {
		if strings.Contains(listed, secret) {
			t.Fatalf("identity response leaked %q: %s", secret, listed)
		}
	}
	if !strings.Contains(listed, `"verification_state":"active"`) {
		t.Fatalf("active state missing from identity response: %s", listed)
	}
	var replyResult string
	var attemptedAt any
	if err := c.db.QueryRow(`SELECT result, attempted_at FROM instagram_verification_replies WHERE external_message_id = 'verify-good'`).Scan(&replyResult, &attemptedAt); err != nil {
		t.Fatal(err)
	}
	if replyResult != "sent" || attemptedAt == nil {
		t.Fatalf("reply record = %q %#v", replyResult, attemptedAt)
	}
}

func TestInstagramIdentityListReturnsSanitizedVerificationFeedback(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")
	registration := registerInstagram(t, c, "alice")
	if registration.VerificationState != "waiting" || registration.VerificationUpdatedAt == nil {
		t.Fatalf("initial verification feedback = %#v", registration)
	}

	if _, err := c.db.Exec(`UPDATE social_identities SET verification_expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), registration.ID); err != nil {
		t.Fatal(err)
	}
	listed := c.request(t, http.MethodGet, "/api/social-identities", nil, false).Body.String()
	if !strings.Contains(listed, `"verification_state":"expired"`) {
		t.Fatalf("expired state missing: %s", listed)
	}

	regenerated := c.request(t, http.MethodPost, "/api/social-identities/"+itoa(int(registration.ID))+"/verification-code", nil, true)
	if regenerated.Code != http.StatusOK {
		t.Fatalf("regenerate = %d %s", regenerated.Code, regenerated.Body.String())
	}
	var replacement instagramRegistration
	if err := json.Unmarshal(regenerated.Body.Bytes(), &replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.VerificationState != "waiting" || replacement.VerificationUpdatedAt == nil {
		t.Fatalf("regenerated feedback = %#v", replacement)
	}
	if _, err := c.db.Exec(`UPDATE social_identities SET verification_consumed_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), registration.ID); err != nil {
		t.Fatal(err)
	}
	simulatedDM(t, c, replacement.InstagramAccount.InstagramUserID, "sender-1", "invalid-consumed", replacement.VerificationCode, nil)
	listed = c.request(t, http.MethodGet, "/api/social-identities", nil, false).Body.String()
	if !strings.Contains(listed, `"verification_state":"invalid_code"`) || strings.Contains(listed, "invalid-consumed") {
		t.Fatalf("invalid state was missing or leaked internals: %s", listed)
	}
}

func TestInstagramReplyFailureIsRecordedWithoutExposingProviderDetails(t *testing.T) {
	attempts := 0
	c := newInstagramReplyTestClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"raw-provider-secret"}}`)),
			Header:     make(http.Header),
		}, nil
	})
	c.register(t, "Alice", "alice@example.com")
	registration := registerInstagram(t, c, "alice")
	response := c.request(t, http.MethodPost, "/api/integrations/instagram/simulated-dm", map[string]string{
		"recipient_instagram_user_id": registration.InstagramAccount.InstagramUserID,
		"sender_platform_user_id":     "sender-1",
		"external_message_id":         "provider-failure",
		"text":                        registration.VerificationCode,
	}, false)
	if response.Code != http.StatusAccepted || strings.Contains(response.Body.String(), "raw-provider-secret") {
		t.Fatalf("failure response = %d %s", response.Code, response.Body.String())
	}
	var replyResult, verificationResult string
	if err := c.db.QueryRow(`SELECT result FROM instagram_verification_replies WHERE external_message_id = 'provider-failure'`).Scan(&replyResult); err != nil {
		t.Fatal(err)
	}
	if err := c.db.QueryRow(`SELECT verification_result FROM social_identities WHERE id = ?`, registration.ID).Scan(&verificationResult); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || replyResult != "system_failure" || verificationResult != "system_failure" {
		t.Fatalf("failure state = attempts %d, reply %q, verification %q", attempts, replyResult, verificationResult)
	}
}

func TestInstagramVerificationFailuresDoNotSendSuccessReply(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *testClient, instagramRegistration)
	}{
		{"invalid code", func(t *testing.T, c *testClient, registration instagramRegistration) {
			simulatedDM(t, c, registration.InstagramAccount.InstagramUserID, "sender-1", "invalid", "not-the-code", nil)
		}},
		{"expired code", func(t *testing.T, c *testClient, registration instagramRegistration) {
			if _, err := c.db.Exec(`UPDATE social_identities SET verification_expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), registration.ID); err != nil {
				t.Fatal(err)
			}
			simulatedDM(t, c, registration.InstagramAccount.InstagramUserID, "sender-1", "expired", registration.VerificationCode, nil)
		}},
		{"unmatched recipient", func(t *testing.T, c *testClient, registration instagramRegistration) {
			simulatedDM(t, c, "wrong-recipient", "sender-1", "unmatched", registration.VerificationCode, nil)
		}},
		{"malformed message", func(t *testing.T, c *testClient, registration instagramRegistration) {
			res := c.request(t, http.MethodPost, "/api/integrations/instagram/simulated-dm", map[string]string{"recipient_instagram_user_id": registration.InstagramAccount.InstagramUserID}, false)
			if res.Code != http.StatusBadRequest || strings.Contains(res.Body.String(), "instagram-user-token") {
				t.Fatalf("malformed response = %d %s", res.Code, res.Body.String())
			}
		}},
		{"already consumed", func(t *testing.T, c *testClient, registration instagramRegistration) {
			if _, err := c.db.Exec(`UPDATE social_identities SET verification_consumed_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), registration.ID); err != nil {
				t.Fatal(err)
			}
			simulatedDM(t, c, registration.InstagramAccount.InstagramUserID, "sender-1", "consumed", registration.VerificationCode, nil)
		}},
		{"system failure", func(t *testing.T, c *testClient, registration instagramRegistration) {
			if err := c.db.Close(); err != nil {
				t.Fatal(err)
			}
			res := c.request(t, http.MethodPost, "/api/integrations/instagram/simulated-dm", map[string]string{
				"recipient_instagram_user_id": registration.InstagramAccount.InstagramUserID,
				"sender_platform_user_id":     "sender-1", "external_message_id": "failure", "text": registration.VerificationCode,
			}, false)
			if res.Code != http.StatusInternalServerError || strings.Contains(res.Body.String(), "instagram-user-token") {
				t.Fatalf("failure response = %d %s", res.Code, res.Body.String())
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			c := newInstagramReplyTestClient(t, func(req *http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			})
			c.register(t, "Alice", "alice@example.com")
			test.run(t, c, registerInstagram(t, c, "alice"))
			if attempts != 0 {
				t.Fatalf("success reply attempts = %d, want 0", attempts)
			}
		})
	}
}
