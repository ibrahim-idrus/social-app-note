package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestFacebookWebhookPostIsNotRegistered(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	w := httptest.NewRecorder()
	Handler(db, Options{FacebookWebhookVerifyToken: "facebook-secret"}).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/integrations/facebook/webhook", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
