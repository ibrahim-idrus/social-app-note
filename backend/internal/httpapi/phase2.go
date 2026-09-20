package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"social-notes/backend/internal/store"
)

func (api *API) socialPlatforms(w http.ResponseWriter, _ *http.Request, _ authentication) {
	inbox, _ := store.GetInstagramIntegration(context.Background(), api.db)
	writeJSON(w, http.StatusOK, map[string]any{"platforms": []map[string]any{{
		"id": "instagram", "name": "Instagram", "available": true, "search_enabled": false, "inbox": inbox,
	}}})
}

type instagramAccountMatch struct {
	ID                string `json:"id"`
	Username          string `json:"username"`
	Name              string `json:"name"`
	ProfilePictureURL string `json:"profile_picture_url"`
}

var errInstagramAuth = errors.New("instagram authentication failed")

func (api *API) discoverInstagramAccount(ctx context.Context, username string) (*instagramAccountMatch, error) {
	endpoint := fmt.Sprintf("https://graph.instagram.com/%s/me", url.PathEscape(api.instagramGraphVersion))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	query := req.URL.Query()
	query.Set("fields", "id,username,name,profile_picture_url")
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Authorization", "Bearer "+api.instagramAccessToken)
	res, err := api.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var provider struct {
			Error struct {
				Type string `json:"type"`
				Code int    `json:"code"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, maxBodyBytes)).Decode(&provider) == nil && (provider.Error.Type == "OAuthException" || provider.Error.Code == 190) {
			return nil, errInstagramAuth
		}
		return nil, errors.New("instagram search failed")
	}
	var account instagramAccountMatch
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBodyBytes)).Decode(&account); err != nil {
		return nil, errors.New("instagram search failed")
	}
	accountUsername, ok := normalizeInstagramUsername(account.Username)
	if !ok || accountUsername != username {
		return nil, nil
	}
	return &account, nil
}

func instagramSearchError(err error) string {
	if errors.Is(err, errInstagramAuth) {
		return "instagram_auth_failed"
	}
	return "instagram_search_failed"
}

func (api *API) searchSocialIdentities(w http.ResponseWriter, r *http.Request, _ authentication) {
	username, ok := normalizeInstagramUsername(r.URL.Query().Get("username"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	if api.instagramAccessToken == "" {
		writeError(w, http.StatusServiceUnavailable, "instagram_search_unavailable")
		return
	}
	account, err := api.discoverInstagramAccount(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusBadGateway, instagramSearchError(err))
		return
	}
	if account == nil {
		writeJSON(w, http.StatusOK, map[string]any{"accounts": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": []any{account}})
}

func (api *API) listSocialIdentities(w http.ResponseWriter, r *http.Request, auth authentication) {
	identities, err := store.ListSocialIdentities(r.Context(), api.db, auth.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"identities": identities})
}

func (api *API) createSocialIdentity(w http.ResponseWriter, r *http.Request, auth authentication) {
	var input struct {
		Platform       string `json:"platform"`
		PlatformUserID string `json:"platform_user_id"`
		Username       string `json:"username"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	username, ok := normalizeInstagramUsername(input.Username)
	if input.Platform != "instagram" || input.PlatformUserID != "" || !ok {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	inbox, err := store.GetInstagramIntegration(r.Context(), api.db)
	if err == nil && strings.EqualFold(username, inbox.Username) {
		writeError(w, http.StatusConflict, "dedicated_instagram_account")
		return
	}
	code, codeHash, expiresAt, err := newInstagramVerification()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	identity, err := store.CreatePendingSocialIdentity(r.Context(), api.db, auth.ID, input.Platform, "", username, "", "", codeHash, expiresAt)
	if errors.Is(err, store.ErrPlatformIdentityAlreadyRegistered) {
		writeError(w, http.StatusConflict, "platform_identity_already_registered")
		return
	}
	if errors.Is(err, store.ErrIdentityUnavailable) {
		writeError(w, http.StatusConflict, "identity_unavailable")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	identity.VerificationCode = code
	writeJSON(w, http.StatusCreated, identity)
}

func (api *API) regenerateSocialIdentityCode(w http.ResponseWriter, r *http.Request, auth authentication) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "identity_not_found")
		return
	}
	code, codeHash, expiresAt, err := newInstagramVerification()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	identity, err := store.ReplaceSocialIdentityVerification(r.Context(), api.db, auth.ID, id, codeHash, expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "identity_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	identity.VerificationCode = code
	writeJSON(w, http.StatusOK, identity)
}

func (api *API) simulatedInstagramDM(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RecipientInstagramUserID string `json:"recipient_instagram_user_id"`
		SenderPlatformUserID     string `json:"sender_platform_user_id"`
		ExternalMessageID        string `json:"external_message_id"`
		Text                     string `json:"text"`
		MessageType              string `json:"message_type"`
		IsSelf                   bool   `json:"is_self"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.RecipientInstagramUserID == "" || input.SenderPlatformUserID == "" || input.ExternalMessageID == "" ||
		len(input.RecipientInstagramUserID) > 128 || len(input.SenderPlatformUserID) > 128 ||
		len(input.ExternalMessageID) > 256 || len(input.Text) > maxBodyBytes {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if input.MessageType == "" {
		input.MessageType = "text"
	}
	if !input.IsSelf && input.MessageType == "text" && input.Text != "" {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		outcome, err := store.ProcessInstagramDM(r.Context(), api.db, input.RecipientInstagramUserID, input.SenderPlatformUserID, input.ExternalMessageID, input.Text, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		if outcome.OwnsReply {
			sent := api.sendInstagramText(r.Context(), input.SenderPlatformUserID, fmt.Sprintf("Your @%s Instagram account is connected to %s.", outcome.Username, api.productName)) == nil
			if err := store.RecordInstagramVerificationReply(r.Context(), api.db, input.ExternalMessageID, time.Now().UTC().Format(time.RFC3339Nano), sent); err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (api *API) sendInstagramText(ctx context.Context, recipientID, text string) error {
	if api.instagramAccountID == "" || api.instagramAccessToken == "" {
		return errors.New("instagram messaging is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	payload, err := json.Marshal(map[string]any{
		"recipient": map[string]string{"id": recipientID},
		"message":   map[string]string{"text": text},
	})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("https://graph.instagram.com/%s/%s/messages", url.PathEscape(api.instagramGraphVersion), url.PathEscape(api.instagramAccountID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+api.instagramAccessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := api.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10)); err != nil {
		return err
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return errors.New("instagram send failed")
	}
	return nil
}

func newInstagramVerification() (string, []byte, string, error) {
	secret := make([]byte, 9)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, "", err
	}
	code := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(code))
	expiresAt := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)
	return code, hash[:], expiresAt, nil
}

func (api *API) deleteSocialIdentity(w http.ResponseWriter, r *http.Request, auth authentication) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "identity_not_found")
		return
	}
	err = store.DeleteSocialIdentity(r.Context(), api.db, auth.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "identity_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func normalizeInstagramUsername(value string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	value = strings.ToLower(value)
	if len(value) < 1 || len(value) > 30 || value[0] == '.' || value[len(value)-1] == '.' || strings.Contains(value, "..") {
		return "", false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '.' && char != '_' {
			return "", false
		}
	}
	return value, true
}
