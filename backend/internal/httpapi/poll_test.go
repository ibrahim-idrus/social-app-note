package httpapi

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"social-notes/backend/internal/store"
)

func TestPollSavesEveryInboundDMUnderOwner(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	owner, err := store.CreateUser(ctx, db, "Owner", "owner@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfigureInstagramIntegration(ctx, db, "inbox-id", "akun_testing911", "token"); err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"data":[]}`
		switch {
		case strings.HasSuffix(req.URL.Path, "/conversations"):
			body = `{"data":[{"id":"conv-1"}]}`
		case strings.HasSuffix(req.URL.Path, "/conv-1"):
			body = `{"messages":{"data":[{"id":"msg-1"},{"id":"msg-own"},{"id":"msg-empty"}]}}`
		case strings.HasSuffix(req.URL.Path, "/msg-1"):
			body = `{"id":"msg-1","message":"hello inbox","from":{"id":"stranger-1","username":"stranger"}}`
		case strings.HasSuffix(req.URL.Path, "/msg-own"):
			body = `{"id":"msg-own","message":"my own reply","from":{"id":"inbox-id","username":"akun_testing911"}}`
		case strings.HasSuffix(req.URL.Path, "/msg-empty"):
			body = `{"id":"msg-empty","from":{"id":"stranger-1","username":"stranger"}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	api := &API{db: db, instagramAccountID: "inbox-id", instagramAccessToken: "token",
		instagramGraphVersion: "v26.0", httpClient: &http.Client{Transport: transport}, inboxOwnerEmail: "owner@example.com"}

	if saved, err := api.pollInstagramInbox(ctx); err != nil || saved != 1 {
		t.Fatalf("poll = %d, %v; want 1, nil", saved, err)
	}
	notes, err := store.ListNotes(ctx, db, owner.ID, "", "instagram", "created_at", "asc", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if notes.Total != 1 || !strings.Contains(notes.Notes[0].ContentMarkdown, "hello inbox") ||
		!strings.Contains(notes.Notes[0].Title, "stranger") {
		t.Fatalf("notes = %#v", notes)
	}
	if saved, err := api.pollInstagramInbox(ctx); err != nil || saved != 0 {
		t.Fatalf("re-poll = %d, %v; want 0, nil (dedup)", saved, err)
	}
}
