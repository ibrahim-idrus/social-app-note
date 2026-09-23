package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

func (api *API) processInstagramText(ctx context.Context, recipient, sender, messageID, text string, fallback bool) error {
	// ID contract: recipient is the dedicated inbox, sender is the user.
	// Only recipient resolves the inbox (ProcessInstagramDM + owner lookup);
	// sender resolves the identity via platform_user_id and is never used
	// to find the inbox.
	log.Printf("instagram webhook dispatch recipientID=%q senderID=%q dedicatedID=%q user2ID=%q messageID=%q", recipient, sender, api.instagramAccountID, api.instagramUser2AccountID, messageID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	outcome, err := store.ProcessInstagramDM(ctx, api.db, recipient, sender, messageID, text, now)
	if err != nil {
		return err
	}
	if outcome.Kind == store.InstagramDMUnmatched && fallback {
		ownerID, err := store.InstagramIntegrationOwnerID(ctx, api.db, recipient)
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("instagram inbox lookup ErrNoRows recipientID=%q dedicatedID=%q: no active integration row; senderID=%q is never used for inbox lookup", recipient, api.instagramAccountID, sender)
			return nil
		}
		if err != nil {
			return err
		}
		_, err = store.SaveInstagramInboxNote(ctx, api.db, ownerID, sender, messageID, text)
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
		Object string `json:"object"`
		Entry  []struct {
			ID        string `json:"id"`
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
	if payload.Object != "instagram" {
		w.WriteHeader(http.StatusOK)
		return
	}
	for _, entry := range payload.Entry {
		for _, event := range entry.Messaging {
			log.Printf("instagram webhook received entry.id=%q sender.id=%q recipient.id=%q dedicatedID=%q user2ID=%q messageID=%q", entry.ID, event.Sender.ID, event.Recipient.ID, api.instagramAccountID, api.instagramUser2AccountID, event.Message.MID)
			if event.Message.IsEcho || event.Message.Text == "" || event.Message.MID == "" || event.Sender.ID == "" || event.Recipient.ID == "" || event.Sender.ID == event.Recipient.ID {
				continue
			}
			if api.instagramAccountID != "" && event.Recipient.ID != api.instagramAccountID {
				log.Printf("instagram webhook recipient mismatch recipientID=%q dedicatedID=%q senderID=%q: routing by recipient via integration lookup, sender never used for inbox", event.Recipient.ID, api.instagramAccountID, event.Sender.ID)
			} else {
				log.Printf("instagram webhook recipient matched recipientID=%q dedicatedID=%q senderID=%q", event.Recipient.ID, api.instagramAccountID, event.Sender.ID)
			}
			if err := api.processInstagramText(r.Context(), event.Recipient.ID, event.Sender.ID, event.Message.MID, event.Message.Text, true); err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}
