package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"social-notes/backend/internal/store"
)

type Options struct {
	SecureCookies                                                              bool
	InstagramAccountID, InstagramUsername, InstagramAccessToken                string
	InstagramUser1AccountID, InstagramUser1Username, InstagramAccessUser1Token string
	InstagramUser2AccountID, InstagramUser2Username, InstagramAccessUser2Token string
	InboxOwnerEmail                                                            string
	InstagramWebhookVerifyToken, InstagramAppSecret                            string
	InstagramGraphVersion, ProductName                                         string
	HTTPClient                                                                 *http.Client
}

func Handler(db *sql.DB, options ...Options) http.Handler {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	client := option.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	if option.InstagramGraphVersion == "" {
		option.InstagramGraphVersion = "v26.0"
	}
	if option.ProductName == "" {
		option.ProductName = "NoteDesk"
	}
	api := &API{db: db, secureCookies: option.SecureCookies, limiter: newLoginLimiter(), instagramAccountID: option.InstagramAccountID, instagramAccessToken: option.InstagramAccessToken, instagramGraphVersion: option.InstagramGraphVersion, productName: option.ProductName, httpClient: client, instagramSenders: []instagramSender{{option.InstagramUser1AccountID, option.InstagramUser1Username, option.InstagramAccessUser1Token}, {option.InstagramUser2AccountID, option.InstagramUser2Username, option.InstagramAccessUser2Token}}, inboxOwnerEmail: option.InboxOwnerEmail, instagramWebhookVerifyToken: option.InstagramWebhookVerifyToken, instagramAppSecret: option.InstagramAppSecret}
	_ = store.ConfigureInstagramIntegration(context.Background(), db, option.InstagramAccountID, option.InstagramUsername, option.InstagramAccessToken)
	_ = store.ConfigureInstagramIntegrationOwner(context.Background(), db, option.InstagramAccountID, option.InboxOwnerEmail)
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
	mux.HandleFunc("POST /api/auth/logout", api.authenticated(api.logout, false))
	mux.HandleFunc("GET /api/profile", api.authenticated(api.profile, false))
	mux.HandleFunc("GET /api/social-platforms", api.authenticated(api.socialPlatforms, false))
	mux.HandleFunc("GET /api/social-identities", api.authenticated(api.listSocialIdentities, false))

	mux.HandleFunc("POST /api/social-identities", api.authenticated(api.createSocialIdentity, true))
	mux.HandleFunc("POST /api/social-identities/{id}/verification-code", api.authenticated(api.regenerateSocialIdentityCode, true))
	mux.HandleFunc("DELETE /api/social-identities/{id}", api.authenticated(api.deleteSocialIdentity, true))
	mux.HandleFunc("POST /api/integrations/instagram/simulated-dm", api.simulatedInstagramDM)
	mux.HandleFunc("GET /api/integrations/instagram/webhook", api.instagramWebhookVerify)
	mux.HandleFunc("POST /api/integrations/instagram/webhook", api.instagramWebhook)
	mux.HandleFunc("GET /api/notes", api.authenticated(api.listNotes, false))
	mux.HandleFunc("POST /api/notes", api.authenticated(api.createNote, true))
	mux.HandleFunc("GET /api/notes/{id}", api.authenticated(api.getNote, false))
	mux.HandleFunc("PUT /api/notes/{id}", api.authenticated(api.updateNote, true))
	mux.HandleFunc("DELETE /api/notes/{id}", api.authenticated(api.deleteNote, true))
	return mux
}
