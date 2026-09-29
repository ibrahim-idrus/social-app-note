package httpapi

import (
	"crypto/hmac"
	"net/http"
)

func (api *API) facebookWebhookVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if api.facebookWebhookVerifyToken == "" || q.Get("hub.mode") != "subscribe" || q.Get("hub.challenge") == "" || !hmac.Equal([]byte(q.Get("hub.verify_token")), []byte(api.facebookWebhookVerifyToken)) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(q.Get("hub.challenge")))
}
