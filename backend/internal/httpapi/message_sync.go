package httpapi

import (
	"context"
	"database/sql"
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

const syncLease = 2 * time.Minute

func providerEventTime(milliseconds int64) string {
	if milliseconds > 0 {
		return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

type graphMessagePage struct {
	Data []struct {
		ID          string `json:"id"`
		Message     string `json:"message"`
		CreatedTime string `json:"created_time"`
		From        struct {
			ID string `json:"id"`
		} `json:"from"`
		To struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"to"`
		Attachments json.RawMessage `json:"attachments"`
	} `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

func (api *API) fetchInstagramHistory(ctx context.Context, senderID, boundary string) ([]store.QueuedSocialMessage, bool, error) {
	return api.fetchMetaHistory(ctx, "instagram", "https://graph.instagram.com", api.instagramGraphVersion, api.instagramAccountID, senderID, boundary, api.instagramAccessToken)
}

func (api *API) fetchFacebookHistory(ctx context.Context, senderID, boundary string) ([]store.QueuedSocialMessage, bool, error) {
	return api.fetchMetaHistory(ctx, "facebook", "https://graph.facebook.com", api.facebookGraphVersion, api.facebookPageID, senderID, boundary, api.facebookPageAccessToken)
}

func (api *API) fetchMetaHistory(ctx context.Context, platform, host, version, accountID, senderID, boundary, token string) ([]store.QueuedSocialMessage, bool, error) {
	if accountID == "" || senderID == "" || token == "" {
		return nil, false, errors.New("provider unavailable")
	}
	conversationID, err := api.findMetaConversation(ctx, host, version, accountID, senderID, token)
	if err != nil {
		return nil, false, err
	}
	u := host + "/" + url.PathEscape(version) + "/" + url.PathEscape(conversationID) + "/messages"
	var result []store.QueuedSocialMessage
	for pages := 0; u != "" && pages < 20; pages++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, false, errors.New("provider request failed")
		}
		q := req.URL.Query()
		q.Set("fields", "id,created_time,from,to,message,attachments")
		q.Set("limit", "100")
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := api.httpClient.Do(req)
		if err != nil {
			return nil, false, errors.New("provider request failed")
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, false, fmt.Errorf("provider request status %d", res.StatusCode)
		}
		var page graphMessagePage
		err = json.NewDecoder(io.LimitReader(res.Body, maxBodyBytes)).Decode(&page)
		res.Body.Close()
		if err != nil {
			return nil, false, errors.New("provider response invalid")
		}
		for _, m := range page.Data {
			if m.ID == boundary {
				return result, true, nil
			}
			recipient := ""
			if len(m.To.Data) > 0 {
				recipient = m.To.Data[0].ID
			}
			if m.ID == "" || m.CreatedTime == "" || m.From.ID != senderID || recipient != accountID {
				continue
			}
			result = append(result, store.QueuedSocialMessage{Platform: platform, ExternalMessageID: m.ID, ProviderSentAt: m.CreatedTime, SenderID: m.From.ID, RecipientID: recipient, Text: m.Message, AttachmentsJSON: nullJSON(m.Attachments)})
		}
		u = safeNext(page.Paging.Next, host)
	}
	return result, boundary == "" && u == "", nil
}

func (api *API) findMetaConversation(ctx context.Context, host, version, accountID, senderID, token string) (string, error) {
	u := host + "/" + url.PathEscape(version) + "/" + url.PathEscape(accountID) + "/conversations"
	for pages := 0; u != "" && pages < 20; pages++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", errors.New("provider request failed")
		}
		q := req.URL.Query()
		q.Set("fields", "id,participants")
		q.Set("user_id", senderID)
		q.Set("limit", "100")
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := api.httpClient.Do(req)
		if err != nil {
			return "", errors.New("provider request failed")
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return "", fmt.Errorf("provider request status %d", res.StatusCode)
		}
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Paging struct {
				Next string `json:"next"`
			} `json:"paging"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, maxBodyBytes)).Decode(&page)
		res.Body.Close()
		if err != nil {
			return "", errors.New("provider response invalid")
		}
		if len(page.Data) > 0 && page.Data[0].ID != "" {
			return page.Data[0].ID, nil
		}
		u = safeNext(page.Paging.Next, host)
	}
	return "", errors.New("provider conversation unavailable")
}

func nullJSON(raw json.RawMessage) sql.NullString {
	if len(raw) == 0 || string(raw) == "null" {
		return sql.NullString{}
	}
	return sql.NullString{String: string(raw), Valid: true}
}
func safeNext(value, host string) string {
	u, err := url.Parse(value)
	base, _ := url.Parse(host)
	if err != nil || u.Scheme != "https" || u.Host != base.Host {
		return ""
	}
	return u.String()
}

func (api *API) syncMessages(w http.ResponseWriter, r *http.Request, auth authentication) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	identity, err := store.OwnedActiveSocialIdentity(r.Context(), api.db, auth.ID, id)
	if err != nil || identity.Status != "active" || identity.PlatformUserID == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	ok, err := store.AcquireSocialMessageLock(r.Context(), api.db, id, time.Now().UTC(), syncLease)
	if err != nil {
		writeError(w, 500, "internal_error")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "sync_in_progress")
		return
	}
	defer store.ReleaseSocialMessageLock(context.Background(), api.db, id)
	boundary, _ := store.LatestCapturedMessageID(r.Context(), api.db, id, identity.Platform)
	var messages []store.QueuedSocialMessage
	complete := false
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if identity.Platform == "instagram" {
		messages, complete, err = api.fetchInstagramHistory(ctx, *identity.PlatformUserID, boundary)
	} else if identity.Platform == "facebook" {
		messages, complete, err = api.fetchFacebookHistory(ctx, *identity.PlatformUserID, boundary)
	} else {
		writeError(w, http.StatusConflict, "unsupported_provider")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "provider_unavailable")
		return
	}
	fetched := 0
	duplicateFetch := 0
	for _, m := range messages {
		m.SocialIdentityID = id
		added, e := store.EnqueueSocialMessage(r.Context(), api.db, m)
		if e != nil {
			writeError(w, 500, "internal_error")
			return
		}
		if added {
			fetched++
		} else {
			duplicateFetch++
		}
	}
	processed, duplicates, drainErr := api.drainIdentity(r.Context(), id)
	remaining, _ := store.CountSocialMessages(r.Context(), api.db, id)
	if drainErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "processing_failed", "fetched": fetched, "processed": processed, "duplicates": duplicates + duplicateFetch, "remaining": remaining, "complete": complete})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fetched": fetched, "processed": processed, "duplicates": duplicates + duplicateFetch, "remaining": remaining, "oldest_available_at": nil, "complete": complete})
}

func (api *API) drainIdentity(ctx context.Context, id int64) (int, int, error) {
	return store.DrainSocialMessages(ctx, api.db, id, func(m store.QueuedSocialMessage) (bool, error) {
		if m.Platform == "instagram" {
			var a []store.InstagramAttachment
			if m.AttachmentsJSON.Valid {
				_ = json.Unmarshal([]byte(m.AttachmentsJSON.String), &a)
			}
			out, e := store.ProcessInstagramMessage(ctx, api.db, m.RecipientID, m.SenderID, m.ExternalMessageID, m.Text, a, m.ProviderSentAt)
			return out.Kind == store.InstagramDMDuplicate, e
		}
		var a []store.FacebookAttachment
		if m.AttachmentsJSON.Valid {
			_ = json.Unmarshal([]byte(m.AttachmentsJSON.String), &a)
		}
		out, e := store.ProcessFacebookMessage(ctx, api.db, m.RecipientID, m.SenderID, m.ExternalMessageID, m.Text, false, a, m.ProviderSentAt)
		return out.Kind == store.FacebookMessageDuplicate, e
	})
}

func (api *API) queueAndDrain(ctx context.Context, m store.QueuedSocialMessage) (handled bool, result string, err error) {
	id, err := store.ActiveSocialIdentityForMessage(ctx, api.db, m.Platform, m.SenderID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	m.SocialIdentityID = id
	ok, err := store.AcquireSocialMessageLock(ctx, api.db, id, time.Now().UTC(), syncLease)
	if err != nil {
		return true, "", err
	}
	if _, err = store.EnqueueSocialMessage(ctx, api.db, m); err != nil {
		return true, "", err
	}
	if !ok {
		return true, "queued", nil
	}
	defer store.ReleaseSocialMessageLock(context.Background(), api.db, id)
	processed, duplicates, err := api.drainIdentity(ctx, id)
	if err != nil {
		return true, "", err
	}
	if duplicates > 0 && processed == 0 {
		return true, "duplicate", nil
	}
	return true, "note_created", nil
}

var _ = strings.TrimSpace
