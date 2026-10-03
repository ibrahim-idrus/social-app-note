package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
		MID         string                       `json:"mid"`
		Text        string                       `json:"text"`
		IsEcho      bool                         `json:"is_echo"`
		Attachments []instagramWebhookAttachment `json:"attachments"`
	} `json:"message"`
}

type instagramWebhookAttachment struct {
	Type    string                            `json:"type"`
	Payload instagramWebhookAttachmentPayload `json:"payload"`
}

type instagramWebhookAttachmentPayload struct {
	InstagramMediaID string          `json:"ig_post_media_id"`
	ReelVideoID      string          `json:"reel_video_id"`
	Title            string          `json:"title"`
	URL              string          `json:"url"`
	Permalink        string          `json:"permalink"`
	Generic          json.RawMessage `json:"generic"`
	Keys             []string        `json:"-"`
}

func (p *instagramWebhookAttachmentPayload) UnmarshalJSON(data []byte) error {
	type plain instagramWebhookAttachmentPayload
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for key := range fields {
		value.Keys = append(value.Keys, webhookName(key))
	}
	sort.Strings(value.Keys)
	*p = instagramWebhookAttachmentPayload(value)
	return nil
}

type instagramWebhookPayload struct {
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

func (api *API) lookupInstagramMessage(ctx context.Context, messageID string) (json.RawMessage, error) {
	if api.instagramAccessToken == "" || messageID == "" {
		return nil, nil
	}
	endpoint := fmt.Sprintf("https://graph.instagram.com/%s/%s", url.PathEscape(api.instagramGraphVersion), url.PathEscape(messageID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	query := req.URL.Query()
	query.Set("fields", "message,attachments,shares{data{name,description,type,url,id}}")
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Authorization", "Bearer "+api.instagramAccessToken)
	res, err := api.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instagram message lookup failed: status %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
}

func supportedInstagramAttachments(items []instagramWebhookAttachment) []store.InstagramAttachment {
	result := []store.InstagramAttachment{}
	for _, item := range items {
		u, err := url.Parse(item.Payload.URL)
		permalink := instagramPermalink(item.Payload.Permalink)
		if item.Type == "ig_post" && item.Payload.InstagramMediaID != "" && err == nil && u.Scheme == "https" && u.Hostname() == "lookaside.fbsbx.com" {
			result = append(result, store.InstagramAttachment{Type: item.Type, URL: item.Payload.URL, InstagramMediaID: item.Payload.InstagramMediaID, Permalink: permalink, Alt: item.Payload.Title})
		} else if item.Type == "ig_reel" && item.Payload.ReelVideoID != "" && err == nil && u.Scheme == "https" && (u.Hostname() == "instagram.com" || u.Hostname() == "www.instagram.com") {
			if permalink == "" {
				permalink = item.Payload.URL
			}
			result = append(result, store.InstagramAttachment{Type: item.Type, URL: item.Payload.URL, InstagramMediaID: item.Payload.ReelVideoID, Permalink: permalink, Alt: item.Payload.Title})
		}
	}
	return result
}

func instagramPermalink(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Hostname() != "instagram.com" && u.Hostname() != "www.instagram.com") {
		return ""
	}
	if !strings.HasPrefix(u.Path, "/p/") && !strings.HasPrefix(u.Path, "/reel/") {
		return ""
	}
	return u.String()
}

func instagramLinks(data json.RawMessage) []store.InstagramAttachment {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	seen := map[string]bool{}
	var result []store.InstagramAttachment
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for _, child := range value {
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		case string:
			link := instagramPermalink(value)
			if link == "" || seen[link] {
				return
			}
			seen[link] = true
			typeName := "ig_post"
			if strings.HasPrefix(mustURLPath(link), "/reel/") {
				typeName = "ig_reel"
			}
			result = append(result, store.InstagramAttachment{Type: typeName, URL: link, Permalink: link, Alt: "Shared Instagram link"})
		}
	}
	visit(value)
	return result
}

func mustURLPath(value string) string {
	u, _ := url.Parse(value)
	return u.Path
}

func logUnsupportedInstagramAttachments(items []instagramWebhookAttachment) {
	for index, item := range items {
		u, err := url.Parse(item.Payload.URL)
		scheme, host := "none", "none"
		if err == nil {
			scheme = webhookName(u.Scheme)
			switch u.Hostname() {
			case "lookaside.fbsbx.com":
				host = "meta_cdn"
			case "instagram.com", "www.instagram.com":
				host = "instagram"
			default:
				if u.Hostname() != "" {
					host = "other"
				}
			}
		}
		log.Printf("instagram unsupported attachment index=%d type=%s payload_keys=%s ig_media_id=%t reel_video_id=%t url=%t url_parse_ok=%t scheme=%s host=%s", index, webhookName(item.Type), strings.Join(item.Payload.Keys, ","), item.Payload.InstagramMediaID != "", item.Payload.ReelVideoID != "", item.Payload.URL != "", err == nil, scheme, host)
		if len(item.Payload.Generic) > 0 {
			log.Printf("instagram unsupported attachment index=%d generic_shape=%s", index, jsonShape(item.Payload.Generic, 0))
		}
	}
}

func jsonShape(data json.RawMessage, depth int) string {
	if depth >= 3 {
		return "nested"
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) == nil {
		parts := make([]string, 0, len(object))
		for key, value := range object {
			parts = append(parts, webhookName(key)+":"+jsonShape(value, depth+1))
		}
		sort.Strings(parts)
		return "{" + strings.Join(parts, ",") + "}"
	}
	var array []json.RawMessage
	if json.Unmarshal(data, &array) == nil {
		if len(array) == 0 {
			return "array(0)"
		}
		return fmt.Sprintf("array(%d,%s)", len(array), jsonShape(array[0], depth+1))
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return "invalid"
	}
	switch value.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	default:
		return "null"
	}
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

func (api *API) processInstagramText(ctx context.Context, recipient, sender, messageID, text string) error {
	// ID contract: recipient is the dedicated inbox, sender is the user.
	// sender resolves the identity via platform_user_id and is never used
	// to find the inbox.
	log.Printf("instagram webhook dispatch recipient=%s sender=%s configured_recipient=%s configured_user2=%s message=%s", webhookID(recipient), webhookID(sender), webhookID(api.instagramAccountID), webhookID(api.instagramUser2AccountID), webhookID(messageID))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	outcome, err := store.ProcessInstagramDM(ctx, api.db, recipient, sender, messageID, text, now)
	if err != nil {
		log.Printf("instagram webhook processing error: %v", err)
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
	log.Printf("instagram webhook raw body=%s", body)
	var payload instagramWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		result = "malformed_json"
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
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
			attachments := supportedInstagramAttachments(event.Message.Attachments)
			if !event.Message.IsEcho && api.instagramAccessToken != "" && event.Message.MID != "" {
				diagnostic, lookupErr := api.lookupInstagramMessage(r.Context(), event.Message.MID)
				if lookupErr != nil {
					log.Printf("instagram message diagnostic message=%s result=failed", webhookID(event.Message.MID))
				} else {
					log.Printf("instagram message diagnostic message=%s result=ok bytes=%d shape=%s", webhookID(event.Message.MID), len(diagnostic), jsonShape(diagnostic, 0))
					attachments = append(attachments, instagramLinks(diagnostic)...)
					if reason == "missing_text" && len(attachments) > 0 {
						reason = ""
					}
				}
			}
			if reason != "" {
				if reason == "missing_text" && len(event.Message.Attachments) > 0 {
					logUnsupportedInstagramAttachments(event.Message.Attachments)
				}
				ignored++
				reasons[reason]++
				continue
			}
			log.Printf("instagram webhook event type=text entry=%s sender=%s recipient=%s message=%s", webhookID(entry.ID), webhookID(event.Sender.ID), webhookID(event.Recipient.ID), webhookID(event.Message.MID))
			if len(attachments) > 0 {
				_, err = store.ProcessInstagramMessage(r.Context(), api.db, event.Recipient.ID, event.Sender.ID, event.Message.MID, event.Message.Text, attachments, time.Now().UTC().Format(time.RFC3339Nano))
			} else {
				err = api.processInstagramText(r.Context(), event.Recipient.ID, event.Sender.ID, event.Message.MID, event.Message.Text)
			}
			if err != nil {
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
	case event.Message.Text == "" && len(supportedInstagramAttachments(event.Message.Attachments)) == 0:
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
	if id == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:6])
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
