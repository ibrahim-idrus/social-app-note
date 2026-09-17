package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"social-notes/backend/internal/store"
)

func (api *API) socialPlatforms(w http.ResponseWriter, _ *http.Request, _ authentication) {
	writeJSON(w, http.StatusOK, map[string]any{"platforms": []map[string]any{{
		"id": "instagram", "name": "Instagram", "available": true, "search_enabled": false,
	}}})
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
		Platform string `json:"platform"`
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	username, ok := normalizeInstagramUsername(input.Username)
	if input.Platform != "instagram" || !ok {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	code, codeHash, expiresAt, err := newInstagramVerification()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	identity, err := store.CreatePendingSocialIdentity(r.Context(), api.db, auth.ID, input.Platform, username, codeHash, expiresAt)
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
		if err := store.ProcessInstagramDM(r.Context(), api.db, input.RecipientInstagramUserID, input.SenderPlatformUserID, input.ExternalMessageID, input.Text, now); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
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
