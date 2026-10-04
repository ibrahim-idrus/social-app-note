package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"social-notes/backend/internal/store"
)

const maxInstagramMediaBytes = 25 << 20

type instagramResolvedMedia struct {
	MediaType    string `json:"media_type"`
	MediaURL     string `json:"media_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Children     struct {
		Data []instagramResolvedMedia `json:"data"`
	} `json:"children"`
}

func (api *API) resolveInstagramMedia(w http.ResponseWriter, r *http.Request, auth authentication) {
	noteID, ok := noteID(w, r)
	if !ok {
		return
	}
	note, err := store.NoteByID(r.Context(), api.db, auth.ID, noteID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "note_not_found")
		return
	}
	if err != nil {
		writeError(w, 500, "internal_error")
		return
	}
	if note.ExternalMessageID != nil && api.instagramAccessToken != "" {
		missing := false
		for _, attachment := range note.InstagramAttachments {
			missing = missing || attachment.Permalink == ""
		}
		if missing {
			if raw, lookupErr := api.lookupInstagramMessage(r.Context(), *note.ExternalMessageID); lookupErr == nil {
				links := instagramLinks(raw)
				changed := false
				for i := range note.InstagramAttachments {
					if note.InstagramAttachments[i].Permalink == "" && len(links) > 0 {
						note.InstagramAttachments[i].Permalink = links[0].Permalink
						changed = true
						links = links[1:]
					}
				}
				if changed {
					_ = store.UpdateInstagramAttachments(r.Context(), api.db, auth.ID, noteID, note.InstagramAttachments)
				}
			}
		}
	}
	result := make([]store.InstagramCachedMedia, 0)
	for position, attachment := range note.InstagramAttachments {
		key := fmt.Sprint(position)
		claimed, cached, claimErr := store.ClaimInstagramMedia(r.Context(), api.db, noteID, key, time.Now().UTC())
		if claimErr != nil {
			continue
		}
		if !claimed {
			result = append(result, cached...)
			continue
		}
		items, fetchErr := api.fetchInstagramAttachment(r.Context(), attachment, noteID, position)
		if fetchErr != nil || store.CompleteInstagramMedia(r.Context(), api.db, noteID, key, items, time.Now().UTC()) != nil {
			_ = store.FailInstagramMedia(r.Context(), api.db, noteID, key, time.Now().UTC())
			continue
		}
		result = append(result, items...)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Position < result[j].Position })
	writeJSON(w, http.StatusOK, map[string]any{"media": result, "attachments": note.InstagramAttachments})
}

func (api *API) fetchInstagramAttachment(ctx context.Context, attachment store.InstagramAttachment, noteID int64, position int) ([]store.InstagramCachedMedia, error) {
	if direct, err := url.Parse(attachment.URL); err == nil && direct.Scheme == "https" && safeInstagramMediaHost(direct.Hostname()) {
		kind := "image"
		if attachment.Type == "ig_reel" {
			kind = "video"
		}
		item, err := api.cacheInstagramURL(ctx, attachment.URL, noteID, position, 0, kind)
		if err == nil {
			return []store.InstagramCachedMedia{item}, nil
		}
	}
	if attachment.InstagramMediaID == "" || api.instagramAccessToken == "" {
		return nil, errors.New("missing media reference")
	}
	u := fmt.Sprintf("https://graph.instagram.com/%s/%s?fields=media_type,media_url,thumbnail_url,children{media_type,media_url,thumbnail_url}", url.PathEscape(api.instagramGraphVersion), url.PathEscape(attachment.InstagramMediaID))
	var metadata instagramResolvedMedia
	if err := api.getInstagramJSON(ctx, u, &metadata); err != nil {
		return nil, err
	}
	parts := metadata.Children.Data
	if len(parts) == 0 {
		parts = []instagramResolvedMedia{metadata}
	}
	items := make([]store.InstagramCachedMedia, 0, len(parts))
	for i, part := range parts {
		source, kind := part.MediaURL, "image"
		if strings.EqualFold(part.MediaType, "VIDEO") {
			kind = "video"
		}
		if source == "" {
			source = part.ThumbnailURL
			kind = "image"
		}
		item, err := api.cacheInstagramURL(ctx, source, noteID, position, i, kind)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (api *API) getInstagramJSON(ctx context.Context, raw string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+api.instagramAccessToken)
	res, err := api.instagramMediaClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("metadata status %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxBodyBytes)).Decode(target)
}

func (api *API) cacheInstagramURL(ctx context.Context, raw string, noteID int64, attachment, position int, kind string) (store.InstagramCachedMedia, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !safeInstagramMediaHost(u.Hostname()) {
		return store.InstagramCachedMedia{}, errors.New("unsafe media url")
	}
	res, err := api.instagramMediaClient.Do(mustRequest(ctx, raw))
	if err != nil {
		return store.InstagramCachedMedia{}, err
	}
	defer res.Body.Close()
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(res.Header.Get("Content-Type"), ";")[0]))
	if res.StatusCode != 200 || (!strings.HasPrefix(contentType, "image/") && !strings.HasPrefix(contentType, "video/")) {
		return store.InstagramCachedMedia{}, errors.New("invalid media response")
	}
	ext := filepath.Ext(u.Path)
	if ext == "" {
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%s", noteID, attachment, position, raw)))
	key := hex.EncodeToString(sum[:])
	name := key + ext
	if err := os.MkdirAll(api.instagramMediaCacheDir, 0700); err != nil {
		return store.InstagramCachedMedia{}, err
	}
	tmp, err := os.CreateTemp(api.instagramMediaCacheDir, ".media-")
	if err != nil {
		return store.InstagramCachedMedia{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, copyErr := io.Copy(tmp, io.LimitReader(res.Body, maxInstagramMediaBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil || n > maxInstagramMediaBytes {
		return store.InstagramCachedMedia{}, errors.New("media too large or unwritable")
	}
	if err := os.Rename(tmpName, filepath.Join(api.instagramMediaCacheDir, name)); err != nil {
		return store.InstagramCachedMedia{}, err
	}
	return store.InstagramCachedMedia{CacheKey: name, ContentType: contentType, Kind: kind, Position: attachment*1000 + position}, nil
}

func mustRequest(ctx context.Context, raw string) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	return req
}
func safeInstagramMediaHost(host string) bool {
	return host == "lookaside.fbsbx.com" || host == "scontent.cdninstagram.com" || strings.HasSuffix(host, ".cdninstagram.com") || strings.HasSuffix(host, ".fbcdn.net")
}

func (api *API) readInstagramMedia(w http.ResponseWriter, r *http.Request, auth authentication) {
	noteID, ok := noteID(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if filepath.Base(key) != key || strings.HasPrefix(key, ".") {
		writeError(w, 404, "note_not_found")
		return
	}
	item, err := store.InstagramMediaOwner(r.Context(), api.db, auth.ID, noteID, key)
	if err != nil {
		writeError(w, 404, "note_not_found")
		return
	}
	w.Header().Set("Content-Type", item.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(api.instagramMediaCacheDir, key))
}
