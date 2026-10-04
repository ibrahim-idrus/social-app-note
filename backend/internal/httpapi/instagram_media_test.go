package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"social-notes/backend/internal/store"
)

type mediaRoundTripper func(*http.Request) (*http.Response, error)

func (f mediaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInstagramMediaRoutesRequireOwnerCSRFAndCacheBytes(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		body, contentType := `{"media_type":"CAROUSEL_ALBUM","children":{"data":[{"media_type":"IMAGE","media_url":"https://lookaside.fbsbx.com/first.jpg"},{"media_type":"VIDEO","media_url":"https://lookaside.fbsbx.com/second.mp4","thumbnail_url":"https://lookaside.fbsbx.com/second.jpg"}]}}`, "application/json"
		if r.URL.Host == "graph.instagram.com" && r.URL.Path != "/v26.0/m1" {
			return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"unsupported path"}`)), Request: r}, nil
		}
		if r.URL.Host == "lookaside.fbsbx.com" {
			body = "bytes-" + r.URL.Path
			if strings.HasSuffix(r.URL.Path, ".mp4") {
				contentType = "video/mp4"
			} else {
				contentType = "image/jpeg"
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	a := newTestClientWithOptions(t, false, Options{HTTPClient: client, InstagramAccessToken: "token", InstagramMediaCacheDir: t.TempDir()})
	a.register(t, "Alice", "alice@example.com")
	created := a.request(t, http.MethodPost, "/api/notes", map[string]string{"title": "ig", "content_markdown": ""}, true)
	id := int(createdJSONID(t, created))
	_, err := a.db.Exec(`UPDATE notes SET source='instagram',instagram_attachments_json='[{"type":"ig_post","url":"https://www.instagram.com/p/x/","media_id":"m1","permalink":"https://www.instagram.com/p/x/","alt":"post"}]' WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/notes/" + itoa(id) + "/instagram-media/resolve"
	if got := a.request(t, http.MethodPost, path, nil, false).Code; got != http.StatusForbidden {
		t.Fatalf("csrf=%d", got)
	}
	resolved := a.request(t, http.MethodPost, path, nil, true)
	if resolved.Code != 200 || !strings.Contains(resolved.Body.String(), `"kind":"image"`) || !strings.Contains(resolved.Body.String(), `"kind":"video"`) {
		t.Fatalf("resolve=%d %s", resolved.Code, resolved.Body.String())
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
	again := a.request(t, http.MethodPost, path, nil, true)
	if again.Code != 200 || calls != 3 {
		t.Fatalf("cache status=%d calls=%d", again.Code, calls)
	}
	var key string
	if err := a.db.QueryRow(`SELECT json_extract(media_json,'$[0].cache_key') FROM instagram_media_cache`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	read := a.request(t, http.MethodGet, "/api/notes/"+itoa(id)+"/instagram-media/"+key, nil, false)
	if read.Code != 200 || read.Body.String() != "bytes-/first.jpg" {
		t.Fatalf("read=%d %q", read.Code, read.Body.String())
	}
	b := newTestClientWithOptions(t, false, Options{InstagramMediaCacheDir: t.TempDir()})
	b.register(t, "Bob", "bob@example.com")
	if got := b.request(t, http.MethodGet, "/api/notes/"+itoa(id)+"/instagram-media/"+key, nil, false).Code; got != 404 {
		t.Fatalf("other owner=%d", got)
	}
}

func TestInstagramMediaCachesWebhookURLWithoutGraphLookup(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "lookaside.fbsbx.com" {
			t.Fatalf("unexpected provider lookup: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("post-bytes")), Request: r}, nil
	})}
	a := newTestClientWithOptions(t, false, Options{HTTPClient: client, InstagramAccessToken: "token", InstagramMediaCacheDir: t.TempDir()})
	a.register(t, "Alice", "alice-direct@example.com")
	created := a.request(t, http.MethodPost, "/api/notes", map[string]string{"title": "ig", "content_markdown": ""}, true)
	id := int(createdJSONID(t, created))
	_, err := a.db.Exec(`UPDATE notes SET source='instagram',instagram_attachments_json='[{"type":"ig_post","url":"https://lookaside.fbsbx.com/ig_messaging_cdn/?asset_id=m1","media_id":"m1","alt":"post"}]' WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	resolved := a.request(t, http.MethodPost, "/api/notes/"+itoa(id)+"/instagram-media/resolve", nil, true)
	if resolved.Code != 200 || !strings.Contains(resolved.Body.String(), `"kind":"image"`) || calls != 1 {
		t.Fatalf("resolve=%d calls=%d %s", resolved.Code, calls, resolved.Body.String())
	}
}

func TestInstagramMediaRecoversAndPersistsMissingPermalinkOnDetailLookup(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "graph.instagram.com" {
			if r.URL.Path != "/v26.0/message-mid" {
				t.Fatalf("unexpected provider lookup: %s", r.URL)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"attachments":{"data":[{"url":"https://www.instagram.com/p/recovered/"}]}}`)), Request: r}, nil
		}
		if r.URL.Host != "lookaside.fbsbx.com" {
			t.Fatalf("unexpected media request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("post-bytes")), Request: r}, nil
	})}
	a := newTestClientWithOptions(t, false, Options{HTTPClient: client, InstagramAccessToken: "token", InstagramMediaCacheDir: t.TempDir()})
	a.register(t, "Alice", "alice-permalink@example.com")
	created := a.request(t, http.MethodPost, "/api/notes", map[string]string{"title": "ig", "content_markdown": ""}, true)
	id := int(createdJSONID(t, created))
	_, err := a.db.Exec(`UPDATE notes SET source='instagram',external_message_id='message-mid',instagram_attachments_json='[{"type":"ig_post","url":"https://lookaside.fbsbx.com/media.jpg","media_id":"m1","alt":"post"}]' WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}

	resolved := a.request(t, http.MethodPost, "/api/notes/"+itoa(id)+"/instagram-media/resolve", nil, true)
	if resolved.Code != 200 || calls != 2 {
		t.Fatalf("resolve=%d calls=%d %s", resolved.Code, calls, resolved.Body.String())
	}
	var raw string
	if err := a.db.QueryRow(`SELECT instagram_attachments_json FROM notes WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"permalink":"https://www.instagram.com/p/recovered/"`) {
		t.Fatalf("attachments=%s", raw)
	}
	_ = a.request(t, http.MethodPost, "/api/notes/"+itoa(id)+"/instagram-media/resolve", nil, true)
	if calls != 2 {
		t.Fatalf("successful lookup was not cached: calls=%d", calls)
	}
}

func createdJSONID(t *testing.T, r *httptest.ResponseRecorder) int64 {
	t.Helper()
	var value struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value.ID
}

func newTestClientWithOptions(t *testing.T, secure bool, options Options) *testClient {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	options.SecureCookies = secure
	return &testClient{handler: Handler(db, options), db: db}
}
