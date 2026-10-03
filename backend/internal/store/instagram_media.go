package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type InstagramCachedMedia struct {
	CacheKey    string `json:"cache_key"`
	ContentType string `json:"content_type"`
	Kind        string `json:"kind"`
	Position    int    `json:"position"`
}

func ClaimInstagramMedia(ctx context.Context, db *sql.DB, noteID int64, key string, now time.Time) (bool, []InstagramCachedMedia, error) {
	var status, attempted string
	var raw sql.NullString
	err := db.QueryRowContext(ctx, `SELECT status,attempted_at,media_json FROM instagram_media_cache WHERE note_id=? AND attachment_key=?`, noteID, key).Scan(&status, &attempted, &raw)
	if err == nil && status == "available" {
		var items []InstagramCachedMedia
		err = json.Unmarshal([]byte(raw.String), &items)
		return false, items, err
	}
	if err != nil && err != sql.ErrNoRows {
		return false, nil, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	cutoff := now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	cooldown := now.Add(-5 * time.Minute).UTC().Format(time.RFC3339Nano)
	result, err := db.ExecContext(ctx, `INSERT INTO instagram_media_cache(note_id,attachment_key,status,attempted_at) VALUES(?,?,'fetching',?) ON CONFLICT(note_id,attachment_key) DO UPDATE SET status='fetching',attempted_at=excluded.attempted_at WHERE (status='fetching' AND attempted_at<=?) OR (status='failed' AND attempted_at<=?)`, noteID, key, stamp, cutoff, cooldown)
	if err != nil {
		return false, nil, err
	}
	n, err := result.RowsAffected()
	return n == 1, nil, err
}

func CompleteInstagramMedia(ctx context.Context, db *sql.DB, noteID int64, key string, items []InstagramCachedMedia, now time.Time) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE instagram_media_cache SET status='available',media_json=?,cached_at=? WHERE note_id=? AND attachment_key=? AND status='fetching'`, string(raw), now.UTC().Format(time.RFC3339Nano), noteID, key)
	return err
}
func FailInstagramMedia(ctx context.Context, db *sql.DB, noteID int64, key string, now time.Time) error {
	_, err := db.ExecContext(ctx, `UPDATE instagram_media_cache SET status='failed',media_json=NULL,attempted_at=? WHERE note_id=? AND attachment_key=? AND status='fetching'`, now.UTC().Format(time.RFC3339Nano), noteID, key)
	return err
}
func InstagramMediaOwner(ctx context.Context, db *sql.DB, userID, noteID int64, cacheKey string) (InstagramCachedMedia, error) {
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT c.media_json FROM instagram_media_cache c JOIN notes n ON n.id=c.note_id WHERE c.note_id=? AND n.user_id=? AND c.status='available'`, noteID, userID).Scan(&raw); err != nil {
		return InstagramCachedMedia{}, err
	}
	var items []InstagramCachedMedia
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return InstagramCachedMedia{}, err
	}
	for _, item := range items {
		if item.CacheKey == cacheKey {
			return item, nil
		}
	}
	return InstagramCachedMedia{}, sql.ErrNoRows
}
