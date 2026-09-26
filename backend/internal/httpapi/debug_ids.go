package httpapi

// TEMPORARY DEBUG CODE - remove this entire file plus the one route line in
// health.go and the one recordDebugWebhookIDs call in webhook.go (both marked
// TEMPORARY DEBUG CODE). It exists only to inspect the relationship between
// webhook recipient.id / sender.id and the configured integration recipient.
// Raw IDs live only in process memory, never in logs or the database.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"social-notes/backend/internal/store"
)

// TEMPORARY DEBUG CODE
type debugWebhookIDs struct {
	EntryID     string `json:"entry_id"`
	SenderID    string `json:"sender_id"`
	RecipientID string `json:"recipient_id"`
	MessageMID  string `json:"message_mid"`
	Timestamp   string `json:"timestamp"`
}

// TEMPORARY DEBUG CODE - in-memory only, never persisted.
var (
	debugIDsMu   sync.Mutex
	debugLastIDs *debugWebhookIDs
)

// TEMPORARY DEBUG CODE - snapshot raw IDs in memory; no logging, no DB write.
func recordDebugWebhookIDs(entryID, senderID, recipientID, mid string) {
	debugIDsMu.Lock()
	defer debugIDsMu.Unlock()
	debugLastIDs = &debugWebhookIDs{
		EntryID:     entryID,
		SenderID:    senderID,
		RecipientID: recipientID,
		MessageMID:  mid,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// TEMPORARY DEBUG CODE - test helper only.
func resetDebugWebhookIDs() {
	debugIDsMu.Lock()
	defer debugIDsMu.Unlock()
	debugLastIDs = nil
}

// TEMPORARY DEBUG CODE - read-only: DB read + one GET to /me, never sends messages,
// never resolves messaging-scoped sender IDs (IGSIDs) to usernames.
func (api *API) instagramDebugIDs(w http.ResponseWriter, r *http.Request, _ authentication) {
	debugIDsMu.Lock()
	captured := debugLastIDs
	debugIDsMu.Unlock()

	configuredID := api.instagramAccountID
	integration, integrationErr := store.GetInstagramIntegration(r.Context(), api.db)

	matches := false
	if captured != nil && captured.RecipientID != "" && configuredID != "" {
		matches = captured.RecipientID == configuredID
	}

	diagnostic := api.debugConfiguredRecipient(r.Context())

	configured := map[string]any{"recipient_id": configuredID}
	if integrationErr == nil {
		configured["instagram_user_id"] = integration.InstagramUserID
		configured["username"] = integration.Username
		configured["status"] = integration.Status
	} else {
		configured["lookup_error"] = "integration_unavailable"
	}

	var webhook any
	if captured != nil {
		webhook = map[string]any{
			"recipient_id": captured.RecipientID,
			"sender_id":    captured.SenderID,
			"message_mid":  captured.MessageMID,
			"entry_id":     captured.EntryID,
			"timestamp":    captured.Timestamp,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"captured":                     captured != nil,
		"webhook":                      webhook,
		"configured":                   configured,
		"recipient_matches_configured": matches,
		"diagnostic":                   diagnostic,
		"temporary_debug":              true,
	})
}

// TEMPORARY DEBUG CODE - identifies only the configured recipient via the
// existing /me credentials. Sender IGSIDs are deliberately never looked up:
// the supported API does not resolve them to usernames.
func (api *API) debugConfiguredRecipient(ctx context.Context) map[string]any {
	if api.instagramAccessToken == "" {
		return map[string]any{
			"configured_lookup": "unavailable",
			"note":              "no Instagram access token configured; sender IGSIDs cannot be resolved to usernames via supported API",
		}
	}
	endpoint := "https://graph.instagram.com/" + url.PathEscape(api.instagramGraphVersion) + "/me?fields=id,username"
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(lookupCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return map[string]any{"configured_lookup": "error", "message": "request_build_failed"}
	}
	req.Header.Set("Authorization", "Bearer "+api.instagramAccessToken)
	res, err := api.httpClient.Do(req)
	if err != nil {
		return map[string]any{"configured_lookup": "error", "message": "request_failed"}
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return map[string]any{"configured_lookup": "error", "message": "read_failed"}
	}
	if res.StatusCode != http.StatusOK {
		var provider struct {
			Error struct {
				Message   string `json:"message"`
				Type      string `json:"type"`
				Code      int    `json:"code"`
				Subcode   int    `json:"error_subcode"`
				FBTraceID string `json:"fbtrace_id"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &provider) == nil && provider.Error.Code != 0 {
			return map[string]any{
				"configured_lookup": "error",
				"code":              provider.Error.Code,
				"message":           provider.Error.Message,
			}
		}
		return map[string]any{"configured_lookup": "error", "code": res.StatusCode, "message": "lookup_failed"}
	}
	var account struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if json.Unmarshal(body, &account) != nil || account.ID == "" {
		return map[string]any{
			"configured_lookup": "unresolved",
			"note":              "API did not return account information for the configured recipient",
		}
	}
	return map[string]any{
		"configured_lookup": "resolved",
		"username":          account.Username,
		"id":                account.ID,
		"note":              "sender IGSIDs cannot be resolved to usernames via supported API",
	}
}
