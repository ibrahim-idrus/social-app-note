package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"social-notes/backend/internal/store"
)

type facebookWebhookEvent struct {
	Sender struct {
		ID string `json:"id"`
	} `json:"sender"`
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Message struct {
		MID         string            `json:"mid"`
		Text        string            `json:"text"`
		IsEcho      bool              `json:"is_echo"`
		Attachments []json.RawMessage `json:"attachments"`
	} `json:"message"`
}

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

func (api *API) facebookWebhook(w http.ResponseWriter, r *http.Request) {
	log.Printf("facebook webhook receipt")
	requestResult := "unknown"
	processingRan := false
	sender, recipient, page, messageID := "none", "none", "none", "none"
	defer func() {
		log.Printf("facebook webhook result sender=%s recipient=%s page=%s message=%s processing_ran=%t processing_result=%s", sender, recipient, page, messageID, processingRan, requestResult)
	}()

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		requestResult = "body_read_error"
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if !validFacebookSignature(body, r.Header.Get("X-Hub-Signature-256"), api.facebookAppSecret) {
		requestResult = "signature_invalid"
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string                 `json:"id"`
			Messaging []facebookWebhookEvent `json:"messaging"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		requestResult = "malformed_json"
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if payload.Object != "page" {
		requestResult = "ignored_object"
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, entry := range payload.Entry {
		for _, event := range entry.Messaging {
			sender, recipient, messageID = webhookID(event.Sender.ID), webhookID(event.Recipient.ID), webhookID(event.Message.MID)
			page = webhookID(entry.ID)
			log.Printf("facebook webhook event receipt sender=%s recipient=%s page=%s message=%s", sender, recipient, page, messageID)
			attachments := supportedFacebookAttachments(event.Message.Attachments)
			reason := api.facebookWebhookIgnoreReason(entry.ID, event, attachments)
			if reason != "" {
				if err := store.RecordFacebookMessage(r.Context(), api.db, entry.ID, event.Sender.ID, event.Message.MID, event.Message.Text, "ignored", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
					requestResult = "processing_failed"
					writeError(w, http.StatusInternalServerError, "internal_error")
					return
				}
				log.Printf("facebook webhook event result sender=%s recipient=%s page=%s message=%s processing_ran=false processing_result=%s", sender, recipient, page, messageID, reason)
				continue
			}
			processingRan = true
			outcome, err := store.ProcessFacebookMessage(r.Context(), api.db, entry.ID, event.Sender.ID, event.Message.MID, event.Message.Text, event.Message.IsEcho, attachments, time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				log.Printf("facebook webhook event result sender=%s recipient=%s page=%s message=%s processing_ran=true processing_result=error", sender, recipient, page, messageID)
				requestResult = "processing_failed"
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
			if outcome.OwnsReply {
				sent := api.sendFacebookText(r.Context(), event.Sender.ID, "Your Facebook Messenger account is connected to "+api.productName+".") == nil
				if err := store.RecordFacebookVerificationReply(r.Context(), api.db, event.Message.MID, time.Now().UTC().Format(time.RFC3339Nano), sent); err != nil {
					requestResult = "processing_failed"
					writeError(w, http.StatusInternalServerError, "internal_error")
					return
				}
			}
			requestResult = string(outcome.Kind)
			log.Printf("facebook webhook event result sender=%s recipient=%s page=%s message=%s processing_ran=true processing_result=%s", sender, recipient, page, messageID, outcome.Kind)
		}
	}
	if requestResult == "unknown" {
		requestResult = "ignored"
	}
	w.WriteHeader(http.StatusOK)
}

func supportedFacebookAttachments(items []json.RawMessage) []store.FacebookAttachment {
	attachments := make([]store.FacebookAttachment, 0, len(items))
	for _, raw := range items {
		var attachment struct {
			Type    string `json:"type"`
			Payload struct {
				URL string `json:"url"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &attachment) != nil || attachment.Type != "fallback" {
			continue
		}
		u, err := url.Parse(attachment.Payload.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			continue
		}
		attachments = append(attachments, store.FacebookAttachment{Type: "fallback", URL: attachment.Payload.URL})
	}
	return attachments
}

func (api *API) sendFacebookText(ctx context.Context, recipient, text string) error {
	if api.facebookPageAccessToken == "" {
		return errors.New("facebook page access token unavailable")
	}
	payload, err := json.Marshal(map[string]any{"recipient": map[string]string{"id": recipient}, "messaging_type": "RESPONSE", "message": map[string]string{"text": text}})
	if err != nil {
		return err
	}
	endpoint := "https://graph.facebook.com/" + url.PathEscape(api.facebookGraphVersion) + "/me/messages?access_token=" + url.QueryEscape(api.facebookPageAccessToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := api.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("facebook confirmation failed")
	}
	return nil
}

func validFacebookSignature(body []byte, signature, secret string) bool {
	if secret == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return err == nil && len(got) == sha256.Size && hmac.Equal(got, mac.Sum(nil))
}

func (api *API) facebookWebhookIgnoreReason(pageID string, event facebookWebhookEvent, attachments []store.FacebookAttachment) string {
	switch {
	case pageID == "" || pageID != api.facebookPageID:
		return "wrong_page"
	case event.Message.IsEcho:
		return "echo"
	case event.Sender.ID == "":
		return "missing_sender"
	case event.Recipient.ID == "" || event.Recipient.ID != pageID:
		return "wrong_recipient"
	case event.Message.MID == "":
		return "missing_message_id"
	case strings.TrimSpace(event.Message.Text) == "" && len(attachments) == 0:
		return "missing_text"
	default:
		return ""
	}
}
