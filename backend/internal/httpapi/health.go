package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

type Options struct {
	SecureCookies bool
}

func Handler(db *sql.DB, options ...Options) http.Handler {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	api := &API{db: db, secureCookies: option.SecureCookies, limiter: newLoginLimiter()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/auth/register", api.register)
	mux.HandleFunc("POST /api/auth/login", api.login)
	mux.HandleFunc("POST /api/auth/logout", api.authenticated(api.logout, true))
	mux.HandleFunc("GET /api/profile", api.authenticated(api.profile, false))
	mux.HandleFunc("GET /api/social-platforms", api.authenticated(api.socialPlatforms, false))
	mux.HandleFunc("GET /api/social-identities", api.authenticated(api.listSocialIdentities, false))
	mux.HandleFunc("POST /api/social-identities", api.authenticated(api.createSocialIdentity, true))
	mux.HandleFunc("DELETE /api/social-identities/{id}", api.authenticated(api.deleteSocialIdentity, true))
	mux.HandleFunc("GET /api/notes", api.authenticated(api.listNotes, false))
	mux.HandleFunc("POST /api/notes", api.authenticated(api.createNote, true))
	mux.HandleFunc("GET /api/notes/{id}", api.authenticated(api.getNote, false))
	mux.HandleFunc("PUT /api/notes/{id}", api.authenticated(api.updateNote, true))
	mux.HandleFunc("DELETE /api/notes/{id}", api.authenticated(api.deleteNote, true))
	return mux
}
