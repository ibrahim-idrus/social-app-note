package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"social-notes/backend/internal/store"
)

func (api *API) instagramWebhookVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if api.instagramWebhookVerifyToken == "" || q.Get("hub.mode") != "subscribe" || q.Get("hub.challenge") == "" || !hmac.Equal([]byte(q.Get("hub.verify_token")), []byte(api.instagramWebhookVerifyToken)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(q.Get("hub.challenge")))
}

func (api *API) processInstagramText(ctx context.Context, recipient, sender, messageID, text string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	outcome, err := store.ProcessInstagramDM(ctx, api.db, recipient, sender, messageID, text, now)
	if err != nil {
		return err
	}
	if !outcome.OwnsReply {
		return nil
	}
	sent := api.sendInstagramText(ctx, sender, fmt.Sprintf("Your @%s Instagram account is connected to %s.", outcome.Username, api.productName)) == nil
	return store.RecordInstagramVerificationReply(ctx, api.db, messageID, time.Now().UTC().Format(time.RFC3339Nano), sent)
}

func (api *API) instagramWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	if api.instagramAppSecret == "" || !strings.HasPrefix(signature, "sha256=") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	mac := hmac.New(sha256.New, []byte(api.instagramAppSecret))
	_, _ = mac.Write(body)
	if err != nil || len(got) != sha256.Size || !hmac.Equal(got, mac.Sum(nil)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload struct {
		Entry []struct {
			Messaging []struct {
				Sender struct {
					ID string `json:"id"`
				} `json:"sender"`
				Recipient struct {
					ID string `json:"id"`
				} `json:"recipient"`
				Message struct {
					MID    string `json:"mid"`
					Text   string `json:"text"`
					IsEcho bool   `json:"is_echo"`
				} `json:"message"`
			} `json:"messaging"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	for _, entry := range payload.Entry {
		for _, event := range entry.Messaging {
			if event.Message.IsEcho || event.Message.Text == "" || event.Message.MID == "" || event.Sender.ID == "" || event.Recipient.ID == "" || event.Sender.ID == api.instagramAccountID {
				continue
			}
			if err := api.processInstagramText(r.Context(), event.Recipient.ID, event.Sender.ID, event.Message.MID, event.Message.Text); err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}
