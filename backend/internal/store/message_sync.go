package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type QueuedSocialMessage struct {
	ID, SocialIdentityID                                                     int64
	Platform, ExternalMessageID, ProviderSentAt, SenderID, RecipientID, Text string
	AttachmentsJSON                                                          sql.NullString
}

func EnqueueSocialMessage(ctx context.Context, db *sql.DB, m QueuedSocialMessage) (bool, error) {
	result, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO social_message_sync_queue(social_identity_id,platform,external_message_id,provider_sent_at,sender_id,recipient_id,text,attachments_json) VALUES(?,?,?,?,?,?,?,?)`, m.SocialIdentityID, m.Platform, m.ExternalMessageID, m.ProviderSentAt, m.SenderID, m.RecipientID, m.Text, m.AttachmentsJSON)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func NextSocialMessage(ctx context.Context, db *sql.DB, identityID int64) (QueuedSocialMessage, error) {
	var m QueuedSocialMessage
	err := db.QueryRowContext(ctx, `SELECT id,social_identity_id,platform,external_message_id,provider_sent_at,sender_id,recipient_id,text,attachments_json FROM social_message_sync_queue WHERE social_identity_id=? ORDER BY provider_sent_at,external_message_id LIMIT 1`, identityID).Scan(&m.ID, &m.SocialIdentityID, &m.Platform, &m.ExternalMessageID, &m.ProviderSentAt, &m.SenderID, &m.RecipientID, &m.Text, &m.AttachmentsJSON)
	return m, err
}

func CompleteSocialMessage(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM social_message_sync_queue WHERE id=?`, id)
	return err
}
func FailSocialMessage(ctx context.Context, db *sql.DB, id int64, code string) error {
	_, err := db.ExecContext(ctx, `UPDATE social_message_sync_queue SET status='failed',attempt_count=attempt_count+1,last_error_code=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, code, id)
	return err
}
func CountSocialMessages(ctx context.Context, db *sql.DB, identityID int64) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM social_message_sync_queue WHERE social_identity_id=?`, identityID).Scan(&n)
	return n, err
}

func AcquireSocialMessageLock(ctx context.Context, db *sql.DB, identityID int64, now time.Time, ttl time.Duration) (bool, error) {
	n, expires := now.UTC().Format(time.RFC3339Nano), now.Add(ttl).UTC().Format(time.RFC3339Nano)
	result, err := db.ExecContext(ctx, `INSERT INTO social_message_sync_locks(social_identity_id,claimed_at,lease_expires_at) VALUES(?,?,?) ON CONFLICT(social_identity_id) DO UPDATE SET claimed_at=excluded.claimed_at,lease_expires_at=excluded.lease_expires_at WHERE social_message_sync_locks.lease_expires_at<=excluded.claimed_at`, identityID, n, expires)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
func ReleaseSocialMessageLock(ctx context.Context, db *sql.DB, identityID int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM social_message_sync_locks WHERE social_identity_id=?`, identityID)
	return err
}

func DrainSocialMessages(ctx context.Context, db *sql.DB, identityID int64, process func(QueuedSocialMessage) (bool, error)) (processed, duplicates int, err error) {
	for {
		m, e := NextSocialMessage(ctx, db, identityID)
		if errors.Is(e, sql.ErrNoRows) {
			return processed, duplicates, nil
		}
		if e != nil {
			return processed, duplicates, e
		}
		duplicate, e := process(m)
		if e != nil {
			_ = FailSocialMessage(ctx, db, m.ID, "processor_failed")
			return processed, duplicates, e
		}
		if e = CompleteSocialMessage(ctx, db, m.ID); e != nil {
			return processed, duplicates, e
		}
		if duplicate {
			duplicates++
		} else {
			processed++
		}
	}
}

func LatestCapturedMessageID(ctx context.Context, db *sql.DB, identityID int64, platform string) (string, error) {
	var id string
	var err error
	if platform == "instagram" {
		err = db.QueryRowContext(ctx, `SELECT external_message_id FROM notes WHERE social_identity_id=? AND source='instagram' AND external_message_id IS NOT NULL ORDER BY created_at DESC,id DESC LIMIT 1`, identityID).Scan(&id)
	} else {
		err = db.QueryRowContext(ctx, `SELECT r.external_message_id FROM facebook_message_receipts r JOIN social_identities i ON i.platform_user_id=r.psid WHERE i.id=? AND r.status='note_created' ORDER BY r.received_at DESC LIMIT 1`, identityID).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func ActiveSocialIdentityForMessage(ctx context.Context, db *sql.DB, platform, senderID string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM social_identities WHERE platform=? AND platform_user_id=? AND status='active'`, platform, senderID).Scan(&id)
	return id, err
}
func OwnedActiveSocialIdentity(ctx context.Context, db *sql.DB, userID, identityID int64) (SocialIdentity, error) {
	return socialIdentityByID(ctx, db, userID, identityID)
}
