package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type instagramRegistration struct {
	ID                    int64  `json:"id"`
	Username              string `json:"username"`
	Status                string `json:"status"`
	VerificationCode      string `json:"verification_code"`
	VerificationExpiresAt string `json:"verification_expires_at"`
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

	b := newTestClientWithHandler(t, a.handler)
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
