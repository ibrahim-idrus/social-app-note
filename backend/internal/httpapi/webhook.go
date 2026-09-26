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
	"sort"
	"strings"
	"time"

	"social-notes/backend/internal/store"
)

type instagramWebhookEvent struct {
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
}

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
	log.Printf("instagram webhook dispatch recipient=%s sender=%s configured_recipient=%s configured_user2=%s message=%s", webhookID(recipient), webhookID(sender), webhookID(api.instagramAccountID), webhookID(api.instagramUser2AccountID), webhookID(messageID))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	outcome, err := store.ProcessInstagramDM(ctx, api.db, recipient, sender, messageID, text, now)
	if err != nil {
		return err
	}
	if outcome.Kind == store.InstagramDMUnmatched && fallback {
		ownerID, err := store.InstagramIntegrationOwnerID(ctx, api.db, recipient)
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("instagram inbox lookup result=no_integration recipient=%s configured_recipient=%s sender=%s", webhookID(recipient), webhookID(api.instagramAccountID), webhookID(sender))
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
	log.Printf("instagram webhook receipt")
	result, object := "unknown", "unknown"
	entryCount, changeCount, eventCount, processed, failed, ignored := 0, 0, 0, 0, 0, 0
	reasons := map[string]int{}
	defer func() {
		log.Printf("instagram webhook result=%s object=%s entries=%d changes=%d events=%d processed=%d failed=%d ignored=%d reasons=%q", result, object, entryCount, changeCount, eventCount, processed, failed, ignored, webhookReasons(reasons))
	}()

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		result = "body_read_error"
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	if api.instagramAppSecret == "" || !strings.HasPrefix(signature, "sha256=") {
		result = "signature_invalid"
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	mac := hmac.New(sha256.New, []byte(api.instagramAppSecret))
	_, _ = mac.Write(body)
	if err != nil || len(got) != sha256.Size || !hmac.Equal(got, mac.Sum(nil)) {
		result = "signature_invalid"
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string                  `json:"id"`
			Messaging []instagramWebhookEvent `json:"messaging"`
			Changes   []struct {
				Field string                `json:"field"`
				Value instagramWebhookEvent `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		result = "malformed_json"
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	log.Printf("instagram webhook body=%s", body)
	object = webhookName(payload.Object)
	entryCount = len(payload.Entry)
	for _, entry := range payload.Entry {
		changeCount += len(entry.Changes)
		eventCount += len(entry.Messaging)
	}
	if payload.Object != "instagram" {
		result = "ignored_object"
		ignored = 1
		reasons["object"]++
		w.WriteHeader(http.StatusOK)
		return
	}
	for _, entry := range payload.Entry {
		events := entry.Messaging
		for _, change := range entry.Changes {
			log.Printf("instagram webhook change field=%s", webhookName(change.Field))
			if change.Field != "messages" {
				ignored++
				reasons["unsupported_field"]++
				continue
			}
			events = append(events, change.Value)
			eventCount++
		}
		for _, event := range events {
			reason := instagramWebhookIgnoreReason(event)
			if reason != "" {
				ignored++
				reasons[reason]++
				continue
			}
			log.Printf("instagram webhook event type=text entry=%s sender=%s recipient=%s message=%s", webhookID(entry.ID), webhookID(event.Sender.ID), webhookID(event.Recipient.ID), webhookID(event.Message.MID))
			if err := api.processInstagramText(r.Context(), event.Recipient.ID, event.Sender.ID, event.Message.MID, event.Message.Text, true); err != nil {
				failed++
				result = "processing_failed"
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
			processed++
		}
	}
	result = "ok"
	w.WriteHeader(http.StatusOK)
}

func instagramWebhookIgnoreReason(event instagramWebhookEvent) string {
	switch {
	case event.Message.IsEcho:
		return "echo"
	case event.Message.Text == "":
		return "missing_text"
	case event.Message.MID == "":
		return "missing_message_id"
	case event.Sender.ID == "":
		return "missing_sender"
	case event.Recipient.ID == "":
		return "missing_recipient"
	case event.Sender.ID == event.Recipient.ID:
		return "same_sender_recipient"
	default:
		return ""
	}
}

func webhookID(id string) string {
	// ponytail: temporary raw-ID debug to capture real IGSID, revert to hash after.
	return id
}

func webhookName(name string) string {
	if name == "" {
		return "none"
	}
	if len(name) > 40 || strings.IndexFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.')
	}) >= 0 {
		return "other"
	}
	return name
}

func webhookReasons(reasons map[string]int) string {
	parts := make([]string, 0, len(reasons))
	for reason, count := range reasons {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
