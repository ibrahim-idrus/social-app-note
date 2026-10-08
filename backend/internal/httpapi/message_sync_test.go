package httpapi

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"social-notes/backend/internal/store"
)

func activeIdentity(t *testing.T, c *testClient, userID int64, platform, sender string) int64 {
	t.Helper()
	result, err := c.db.Exec(`INSERT INTO social_identities(user_id,platform,platform_user_id,username,normalized_username,status) VALUES(?,?,?,?,?,'active')`, userID, platform, sender, sender, sender)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}

func TestSyncMessagesEndpointRequiresAuthenticationCSRFAndOwnership(t *testing.T) {
	c := newTestClient(t, false)
	if got := c.request(t, http.MethodPost, "/api/social-identities/1/sync-messages", nil, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("anonymous=%d", got)
	}
	c.register(t, "A", "a@sync.test")
	var owner int64
	if err := c.db.QueryRow(`SELECT id FROM users WHERE email='a@sync.test'`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	id := activeIdentity(t, c, owner, "instagram", "sync-sender")
	path := "/api/social-identities/" + strconv.FormatInt(id, 10) + "/sync-messages"
	if got := c.request(t, http.MethodPost, path, nil, false).Code; got != http.StatusForbidden {
		t.Fatalf("csrf=%d", got)
	}
	other := newTestClient(t, false)
	other.register(t, "B", "b@sync.test")
	if got := other.request(t, http.MethodPost, path, nil, true).Code; got != http.StatusNotFound {
		t.Fatalf("cross-owner=%d", got)
	}
}

func TestFetchInstagramHistoryPagesWithoutTokenInURL(t *testing.T) {
	var calls int
	api := API{instagramAccountID: "inbox", instagramAccessToken: "secret", instagramGraphVersion: "v26.0", httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if strings.Contains(req.URL.String(), "secret") || req.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unsafe auth: %s", req.URL)
		}
		body := `{"data":[{"id":"conversation-1"}]}`
		if calls == 2 {
			body = `{"data":[{"id":"m2","created_time":"2026-01-02T00:00:00Z","from":{"id":"sender"},"to":{"data":[{"id":"inbox"}]},"message":"two"},{"id":"boundary","created_time":"2026-01-01T00:00:00Z","from":{"id":"sender"},"to":{"data":[{"id":"inbox"}]}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	messages, complete, err := api.fetchInstagramHistory(context.Background(), "sender", "boundary")
	if err != nil || !complete || calls != 2 || len(messages) != 1 || messages[0].ExternalMessageID != "m2" {
		t.Fatalf("messages=%#v complete=%v calls=%d err=%v", messages, complete, calls, err)
	}
}

func TestWebhookPipelineSameAccountExclusionAndDifferentAccountIndependence(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "A", "lock@sync.test")
	var owner int64
	_ = c.db.QueryRow(`SELECT id FROM users WHERE email='lock@sync.test'`).Scan(&owner)
	a := activeIdentity(t, c, owner, "instagram", "sender-one")
	b := activeIdentity(t, c, owner, "facebook", "sender-two")
	ok, _ := store.AcquireSocialMessageLock(context.Background(), c.db, a, testNow, timeMinute)
	if !ok {
		t.Fatal("lock a")
	}
	if acquired, err := store.AcquireSocialMessageLock(context.Background(), c.db, a, testNow, timeMinute); err != nil || acquired {
		t.Fatalf("same=%v %v", acquired, err)
	}
	if acquired, err := store.AcquireSocialMessageLock(context.Background(), c.db, b, testNow, timeMinute); err != nil || !acquired {
		t.Fatalf("different=%v %v", acquired, err)
	}
}

var testNow = mustTime("2026-01-01T00:00:00Z")
var timeMinute = durationMinute()

func mustTime(s string) (v time.Time) { v, _ = time.Parse(time.RFC3339, s); return }
func durationMinute() time.Duration   { return time.Minute }

func TestSyncProviderErrorDoesNotLeakTokenOrBody(t *testing.T) {
	api := API{instagramAccountID: "inbox", instagramAccessToken: "secret", instagramGraphVersion: "v26.0", httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`private body secret`)), Header: make(http.Header)}, nil
	})}}
	_, _, err := api.fetchInstagramHistory(context.Background(), "sender", "")
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatalf("err=%v", err)
	}
}
