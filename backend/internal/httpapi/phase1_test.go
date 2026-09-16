package httpapi

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"social-notes/backend/internal/store"
)

type testClient struct {
	handler http.Handler
	db      *sql.DB
	cookie  *http.Cookie
	csrf    string
}

func newTestClient(t *testing.T, secure bool) *testClient {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &testClient{handler: Handler(db, Options{SecureCookies: secure}), db: db}
}

func (c *testClient) request(t *testing.T, method, path string, body any, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &encoded)
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if csrf {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	res := httptest.NewRecorder()
	c.handler.ServeHTTP(res, req)
	return res
}

func (c *testClient) register(t *testing.T, name, email string) map[string]any {
	t.Helper()
	res := c.request(t, http.MethodPost, "/api/auth/register", map[string]string{
		"name": name, "email": email, "password": "correct horse battery staple",
	}, false)
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", res.Code, res.Body.String())
	}
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == sessionCookie {
			c.cookie = cookie
		}
	}
	if c.cookie == nil {
		t.Fatal("missing session cookie")
	}
	var result map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	c.csrf, _ = result["csrf_token"].(string)
	return result
}

func TestRegistrationSessionProfileAndLogout(t *testing.T) {
	c := newTestClient(t, false)
	result := c.register(t, " Alice ", " ALICE@example.com ")
	if c.cookie.Name != "session" || !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteLaxMode || c.cookie.Secure {
		t.Fatalf("unsafe local cookie: %#v", c.cookie)
	}
	if c.csrf == "" {
		t.Fatal("missing CSRF token")
	}
	res := c.request(t, http.MethodPost, "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "correct horse battery staple"}, false)
	if res.Code != http.StatusOK {
		t.Fatalf("login = %d %s", res.Code, res.Body.String())
	}
	var csrfCookie *http.Cookie
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "csrf_token" {
			csrfCookie = cookie
		}
		if cookie.Name == sessionCookie {
			c.cookie = cookie
		}
	}
	if csrfCookie == nil || csrfCookie.HttpOnly || csrfCookie.Value == "" || csrfCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("CSRF cookie = %#v", csrfCookie)
	}
	var loginResult map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &loginResult); err != nil {
		t.Fatal(err)
	}
	c.csrf, _ = loginResult["csrf_token"].(string)
	user := result["user"].(map[string]any)
	if user["name"] != "Alice" || user["email"] != "alice@example.com" {
		t.Fatalf("user = %#v", user)
	}

	profile := c.request(t, http.MethodGet, "/api/profile", nil, false)
	if profile.Code != http.StatusOK || !strings.Contains(profile.Body.String(), `"email":"alice@example.com"`) {
		t.Fatalf("profile = %d %s", profile.Code, profile.Body.String())
	}

	withoutCSRF := c.request(t, http.MethodPost, "/api/auth/logout", nil, false)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF = %d", withoutCSRF.Code)
	}
	logout := c.request(t, http.MethodPost, "/api/auth/logout", nil, true)
	if logout.Code != http.StatusNoContent || len(logout.Result().Cookies()) != 2 {
		t.Fatalf("logout = %d, cookies %#v", logout.Code, logout.Result().Cookies())
	}
	for _, cookie := range logout.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			t.Fatalf("logout did not clear cookie: %#v", cookie)
		}
	}
	if got := c.request(t, http.MethodGet, "/api/profile", nil, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("profile after logout = %d", got)
	}
}

func TestSecureCookieIsConfigurable(t *testing.T) {
	c := newTestClient(t, true)
	res := c.request(t, http.MethodPost, "/api/auth/register", map[string]string{
		"name": "Alice", "email": "alice@example.com", "password": "correct horse battery staple",
	}, false)
	if res.Code != http.StatusCreated {
		t.Fatal(res.Body.String())
	}
	for _, cookie := range res.Result().Cookies() {
		if !cookie.Secure {
			t.Fatalf("HTTPS cookie is not Secure: %#v", cookie)
		}
	}
}

func TestLoginLimiterStateIsBounded(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Now()
	for i := range 1100 {
		limiter.failed(itoa(i), now)
	}
	if len(limiter.attempts) > 1024 {
		t.Fatalf("limiter entries = %d", len(limiter.attempts))
	}
}

func TestPasswordsAreHashedAndLoginErrorsAreGenericAndThrottled(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")
	var passwordHash string
	var tokenHash, csrfHash []byte
	if err := c.db.QueryRow(`SELECT password_hash FROM users WHERE email = 'alice@example.com'`).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if passwordHash == "correct horse battery staple" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("correct horse battery staple")) != nil {
		t.Fatalf("password is not a bcrypt hash: %q", passwordHash)
	}
	if err := c.db.QueryRow(`SELECT token_hash, csrf_hash FROM sessions`).Scan(&tokenHash, &csrfHash); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(tokenHash, []byte(c.cookie.Value)) || bytes.Equal(csrfHash, []byte(c.csrf)) || len(tokenHash) != sha256.Size || len(csrfHash) != sha256.Size {
		t.Fatalf("session secrets stored unsafely: token=%d csrf=%d", len(tokenHash), len(csrfHash))
	}

	for _, email := range []string{"alice@example.com", "missing@example.com"} {
		res := c.request(t, http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": "wrong password"}, false)
		if res.Code != http.StatusUnauthorized || res.Body.String() != "{\"error\":\"invalid_credentials\"}\n" {
			t.Fatalf("login error for %s = %d %q", email, res.Code, res.Body.String())
		}
	}

	var res *httptest.ResponseRecorder
	for range 6 {
		res = c.request(t, http.MethodPost, "/api/auth/login", map[string]string{"email": "throttle@example.com", "password": "wrong password"}, false)
	}
	if res.Code != http.StatusTooManyRequests || res.Body.String() != "{\"error\":\"invalid_credentials\"}\n" {
		t.Fatalf("throttled login = %d %q", res.Code, res.Body.String())
	}
}

func TestRegistrationValidationAndDuplicateEmail(t *testing.T) {
	c := newTestClient(t, false)
	for _, body := range []map[string]string{
		{"name": "", "email": "a@example.com", "password": "long enough"},
		{"name": "Alice", "email": "bad", "password": "long enough"},
		{"name": "Alice", "email": "a@example.com", "password": "short"},
		{"name": strings.Repeat("a", 101), "email": "a@example.com", "password": "long enough"},
	} {
		if got := c.request(t, http.MethodPost, "/api/auth/register", body, false).Code; got != http.StatusBadRequest {
			t.Fatalf("invalid registration status = %d for %#v", got, body)
		}
	}
	c.register(t, "Alice", "alice@example.com")
	res := c.request(t, http.MethodPost, "/api/auth/register", map[string]string{
		"name": "Other", "email": "ALICE@example.com", "password": "correct horse battery staple",
	}, false)
	if res.Code != http.StatusConflict || res.Body.String() != "{\"error\":\"email_unavailable\"}\n" {
		t.Fatalf("duplicate = %d %q", res.Code, res.Body.String())
	}
}

func TestManualNoteCRUDCSRFAndOwnership(t *testing.T) {
	a := newTestClient(t, false)
	a.register(t, "Alice", "alice@example.com")
	created := a.request(t, http.MethodPost, "/api/notes", map[string]string{"title": " First ", "content_markdown": "hello"}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var note map[string]any
	json.Unmarshal(created.Body.Bytes(), &note)
	id := int(note["id"].(float64))
	if note["source"] != "manual" || note["title"] != "First" {
		t.Fatalf("note = %#v", note)
	}

	if got := a.request(t, http.MethodPut, "/api/notes/"+itoa(id), map[string]string{"title": "Changed", "content_markdown": "updated"}, false).Code; got != http.StatusForbidden {
		t.Fatalf("update without CSRF = %d", got)
	}
	updated := a.request(t, http.MethodPut, "/api/notes/"+itoa(id), map[string]string{"title": "Changed", "content_markdown": "updated"}, true)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"title":"Changed"`) {
		t.Fatalf("update = %d %s", updated.Code, updated.Body.String())
	}

	b := newTestClientWithHandler(t, a.handler)
	b.register(t, "Bob", "bob@example.com")
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		var body any
		if method == http.MethodPut {
			body = map[string]string{"title": "Stolen", "content_markdown": "no"}
		}
		if got := b.request(t, method, "/api/notes/"+itoa(id), body, method != http.MethodGet).Code; got != http.StatusNotFound {
			t.Fatalf("user B %s status = %d", method, got)
		}
	}
	if got := a.request(t, http.MethodGet, "/api/notes/"+itoa(id), nil, false).Code; got != http.StatusOK {
		t.Fatalf("owner read = %d", got)
	}
	if got := a.request(t, http.MethodDelete, "/api/notes/"+itoa(id), nil, true).Code; got != http.StatusNoContent {
		t.Fatalf("delete = %d", got)
	}
	if got := a.request(t, http.MethodGet, "/api/notes/"+itoa(id), nil, false).Code; got != http.StatusNotFound {
		t.Fatalf("read deleted = %d", got)
	}
}

func newTestClientWithHandler(t *testing.T, handler http.Handler) *testClient {
	t.Helper()
	return &testClient{handler: handler}
}

func TestNoteListSearchFilterSortPaginationAndValidation(t *testing.T) {
	c := newTestClient(t, false)
	c.register(t, "Alice", "alice@example.com")
	for _, title := range []string{"Bravo", "Alpha", "Charlie"} {
		res := c.request(t, http.MethodPost, "/api/notes", map[string]string{"title": title, "content_markdown": "needle " + title}, true)
		if res.Code != http.StatusCreated {
			t.Fatal(res.Body.String())
		}
	}
	res := c.request(t, http.MethodGet, "/api/notes?q=needle&source=manual&sort=title&order=asc&page=2&page_size=2", nil, false)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"title":"Charlie"`) || strings.Contains(res.Body.String(), `"title":"Alpha"`) {
		t.Fatalf("list = %d %s", res.Code, res.Body.String())
	}
	for _, path := range []string{
		"/api/notes?source=email", "/api/notes?sort=user_id", "/api/notes?order=sideways",
		"/api/notes?page=0", "/api/notes?page_size=101", "/api/notes?q=" + strings.Repeat("x", 201),
	} {
		if got := c.request(t, http.MethodGet, path, nil, false).Code; got != http.StatusBadRequest {
			t.Fatalf("invalid list %q = %d", path, got)
		}
	}
	if got := c.request(t, http.MethodPost, "/api/notes", map[string]string{"title": strings.Repeat("x", 201), "content_markdown": "body"}, true).Code; got != http.StatusBadRequest {
		t.Fatalf("long title = %d", got)
	}
}

func TestNotesAndProfileRequireAuthentication(t *testing.T) {
	c := newTestClient(t, false)
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/profile"}, {http.MethodGet, "/api/notes"}, {http.MethodPost, "/api/notes"},
	} {
		if got := c.request(t, request.method, request.path, map[string]string{}, false).Code; got != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d", request.method, request.path, got)
		}
	}
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var result [20]byte
	i := len(result)
	for value > 0 {
		i--
		result[i] = digits[value%10]
		value /= 10
	}
	return string(result[i:])
}
