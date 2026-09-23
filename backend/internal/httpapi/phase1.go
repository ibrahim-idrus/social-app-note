package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"social-notes/backend/internal/store"
)

const (
	sessionCookie = "session"
	csrfCookie    = "csrf_token"
	sessionTTL    = 7 * 24 * time.Hour
	maxBodyBytes  = 128 << 10
)

var dummyPasswordHash = []byte("$2a$10$7EqJtq98hPqEX7fNZaFWoO5JQ9jM.VgQ6mC4McT8yXQ7x1E2Vt8tK")

type API struct {
	db                                                              *sql.DB
	secureCookies                                                   bool
	limiter                                                         *loginLimiter
	instagramAccountID, instagramAccessToken, instagramGraphVersion string
	// Expected sender for trace logs only. Never used for inbox lookup.
	instagramUser2AccountID                         string
	productName                                     string
	httpClient                                      *http.Client
	instagramWebhookVerifyToken, instagramAppSecret string
}

type authentication struct {
	store.Session
	tokenHash []byte
}

func (api *API) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if !validName(input.Name) || !validEmail(input.Email) || len(input.Password) < 8 || len(input.Password) > 72 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	user, err := store.CreateUser(r.Context(), api.db, input.Name, input.Email, string(hash))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.email") {
			writeError(w, http.StatusConflict, "email_unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	api.startSession(w, r, user, http.StatusCreated)
}

func (api *API) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	key := clientIP(r) + "\x00" + input.Email
	if api.limiter.blocked(key, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "invalid_credentials")
		return
	}
	user, err := store.UserByEmail(r.Context(), api.db, input.Email)
	hash := dummyPasswordHash
	if err == nil {
		hash = []byte(user.PasswordHash)
	}
	passwordErr := bcrypt.CompareHashAndPassword(hash, []byte(input.Password))
	if err != nil || passwordErr != nil {
		api.limiter.failed(key, time.Now())
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	api.limiter.succeeded(key)
	api.startSession(w, r, user, http.StatusOK)
}

func (api *API) startSession(w http.ResponseWriter, r *http.Request, user store.User, status int) {
	token, err := randomSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	csrf, err := randomSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	tokenHash := sha256.Sum256([]byte(token))
	csrfHash := sha256.Sum256([]byte(csrf))
	if err := store.CreateSession(r.Context(), api.db, user.ID, tokenHash[:], csrfHash[:], time.Now().Add(sessionTTL).UTC().Format(time.RFC3339Nano)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: api.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookie, Value: csrf, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		Secure: api.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, status, map[string]any{"user": user, "csrf_token": csrf})
}

func (api *API) authenticated(next func(http.ResponseWriter, *http.Request, authentication), csrfRequired bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "authentication_required")
			return
		}
		tokenHash := sha256.Sum256([]byte(cookie.Value))
		session, err := store.SessionByToken(r.Context(), api.db, tokenHash[:], time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication_required")
			return
		}
		if csrfRequired {
			csrfHash := sha256.Sum256([]byte(r.Header.Get("X-CSRF-Token")))
			if subtle.ConstantTimeCompare(csrfHash[:], session.CSRFHash) != 1 {
				writeError(w, http.StatusForbidden, "csrf_failed")
				return
			}
		}
		auth := authentication{Session: session, tokenHash: tokenHash[:]}
		next(w, r, auth)
	}
}

func (api *API) logout(w http.ResponseWriter, r *http.Request, auth authentication) {
	if err := store.DeleteSession(r.Context(), api.db, auth.tokenHash); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: api.secureCookies, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Path: "/", MaxAge: -1, Secure: api.secureCookies, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) profile(w http.ResponseWriter, _ *http.Request, auth authentication) {
	writeJSON(w, http.StatusOK, map[string]any{"user": auth.User})
}

func (api *API) createNote(w http.ResponseWriter, r *http.Request, auth authentication) {
	title, content, ok := noteInput(w, r)
	if !ok {
		return
	}
	note, err := store.CreateNote(r.Context(), api.db, auth.ID, title, content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusCreated, note)
}

func (api *API) getNote(w http.ResponseWriter, r *http.Request, auth authentication) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	note, err := store.NoteByID(r.Context(), api.db, auth.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "note_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (api *API) updateNote(w http.ResponseWriter, r *http.Request, auth authentication) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	title, content, ok := noteInput(w, r)
	if !ok {
		return
	}
	note, err := store.UpdateNote(r.Context(), api.db, auth.ID, id, title, content)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "note_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (api *API) deleteNote(w http.ResponseWriter, r *http.Request, auth authentication) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	err := store.DeleteNote(r.Context(), api.db, auth.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "note_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) listNotes(w http.ResponseWriter, r *http.Request, auth authentication) {
	values := r.URL.Query()
	query := values.Get("q")
	source := values.Get("source")
	sortField := values.Get("sort")
	order := values.Get("order")
	if sortField == "" {
		sortField = "updated_at"
	}
	if order == "" {
		order = "desc"
	}
	page, pageOK := positiveInt(values.Get("page"), 1, 1_000_000)
	pageSize, sizeOK := positiveInt(values.Get("page_size"), 20, 100)
	validSort := sortField == "title" || sortField == "created_at" || sortField == "updated_at"
	validOrder := order == "asc" || order == "desc"
	validSource := source == "" || source == "manual" || source == "instagram"
	if len(query) > 200 || !validSource || !validSort || !validOrder || !pageOK || !sizeOK {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	result, err := store.ListNotes(r.Context(), api.db, auth.ID, query, source, sortField, order, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": result.Notes, "total": result.Total, "page": page, "page_size": pageSize})
}

func noteInput(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	var input struct {
		Title           string `json:"title"`
		ContentMarkdown string `json:"content_markdown"`
	}
	if !decodeJSON(w, r, &input) {
		return "", "", false
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Title) > 200 || len(input.ContentMarkdown) > 100_000 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return "", "", false
	}
	return input.Title, input.ContentMarkdown, true
}

func noteID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "note_not_found")
		return 0, false
	}
	return id, true
}

func validName(value string) bool {
	return value != "" && len(value) <= 100
}

func validEmail(value string) bool {
	if value == "" || len(value) > 254 {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

func positiveInt(value string, defaultValue, max int) (int, bool) {
	if value == "" {
		return defaultValue, true
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil && parsed >= 1 && parsed <= max
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func randomSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

type loginAttempt struct {
	count int
	start time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (limiter *loginLimiter) blocked(key string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	attempt, ok := limiter.attempts[key]
	if !ok || now.Sub(attempt.start) >= time.Minute {
		delete(limiter.attempts, key)
		return false
	}
	return attempt.count >= 5
}

func (limiter *loginLimiter) failed(key string, now time.Time) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	attempt := limiter.attempts[key]
	if attempt.start.IsZero() || now.Sub(attempt.start) >= time.Minute {
		attempt = loginAttempt{start: now}
	}
	attempt.count++
	limiter.attempts[key] = attempt
	if len(limiter.attempts) > 1024 {
		oldestKey := key
		oldestStart := now
		for candidate, value := range limiter.attempts {
			if now.Sub(value.start) >= time.Minute {
				delete(limiter.attempts, candidate)
				continue
			}
			if !value.start.After(oldestStart) {
				oldestKey = candidate
				oldestStart = value.start
			}
		}
		if len(limiter.attempts) > 1024 {
			delete(limiter.attempts, oldestKey)
		}
	}
}

func (limiter *loginLimiter) succeeded(key string) {
	limiter.mu.Lock()
	delete(limiter.attempts, key)
	limiter.mu.Unlock()
}
