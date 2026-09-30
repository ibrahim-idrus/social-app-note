package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type FacebookIntegration struct {
	PageID string `json:"page_id"`
}

type FacebookMessageOutcomeKind string

const (
	FacebookMessageIgnored     FacebookMessageOutcomeKind = "ignored"
	FacebookMessageDuplicate   FacebookMessageOutcomeKind = "duplicate"
	FacebookMessageActivated   FacebookMessageOutcomeKind = "activated"
	FacebookMessageNoteCreated FacebookMessageOutcomeKind = "note_created"
	FacebookMessageExpired     FacebookMessageOutcomeKind = "expired"
	FacebookMessageInvalidCode FacebookMessageOutcomeKind = "invalid_code"
)

type FacebookMessageOutcome struct {
	Kind       FacebookMessageOutcomeKind
	IdentityID int64
	OwnsReply  bool
}

func ConfigureFacebookIntegration(ctx context.Context, db *sql.DB, pageID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var previousPageID string
	err = tx.QueryRowContext(ctx, `SELECT page_id FROM facebook_integrations WHERE id='configured_page'`).Scan(&previousPageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if pageID == "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM social_identities WHERE platform='facebook'`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM facebook_integrations`); err != nil {
			return err
		}
		return tx.Commit()
	}
	if previousPageID != "" && previousPageID != pageID {
		if _, err := tx.ExecContext(ctx, `DELETE FROM social_identities WHERE platform='facebook'`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO facebook_integrations (id, page_id, status) VALUES ('configured_page', ?, 'active')
		ON CONFLICT(id) DO UPDATE SET page_id=excluded.page_id, status='active', updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, pageID); err != nil {
		return err
	}
	return tx.Commit()
}

func GetFacebookIntegration(ctx context.Context, db *sql.DB) (FacebookIntegration, error) {
	var integration FacebookIntegration
	err := db.QueryRowContext(ctx, `SELECT page_id FROM facebook_integrations WHERE id='configured_page' AND status='active'`).Scan(&integration.PageID)
	return integration, err
}

func CreatePendingFacebookIdentity(ctx context.Context, db *sql.DB, userID int64, username string, codeHash []byte, expiresAt string) (SocialIdentity, error) {
	return CreatePendingSocialIdentity(ctx, db, userID, "facebook", "", username, "", "", codeHash, expiresAt)
}

func ReplaceFacebookIdentityVerification(ctx context.Context, db *sql.DB, userID, id int64, codeHash []byte, expiresAt string) (SocialIdentity, error) {
	result, err := db.ExecContext(ctx, `
		UPDATE social_identities
		SET verification_code_hash=?, verification_expires_at=?, verification_consumed_at=NULL,
			verification_result='waiting', verification_result_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),
			updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND user_id=? AND platform='facebook' AND status='pending'`, codeHash, expiresAt, id, userID)
	if err != nil {
		return SocialIdentity{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		if err != nil {
			return SocialIdentity{}, err
		}
		return SocialIdentity{}, sql.ErrNoRows
	}
	return socialIdentityByID(ctx, db, userID, id)
}

type FacebookMessage struct {
	ExternalMessageID string `json:"external_message_id"`
	SenderID          string `json:"sender_id"`
	PageID            string `json:"page_id"`
	Text              string `json:"text"`
	Status            string `json:"status"`
	NoteID            *int64 `json:"note_id"`
	ReceivedAt        string `json:"received_at"`
}

func ListFacebookMessages(ctx context.Context, db *sql.DB, userID int64) ([]FacebookMessage, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.external_message_id,r.psid,r.page_id,r.message_text,r.status,r.note_id,r.received_at FROM facebook_message_receipts r JOIN social_identities i ON i.platform='facebook' AND i.status='active' AND i.platform_user_id=r.psid WHERE i.user_id=? ORDER BY r.received_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []FacebookMessage{}
	for rows.Next() {
		var m FacebookMessage
		if err := rows.Scan(&m.ExternalMessageID, &m.SenderID, &m.PageID, &m.Text, &m.Status, &m.NoteID, &m.ReceivedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func RecordFacebookMessage(ctx context.Context, db *sql.DB, pageID, psid, externalMessageID, text, status, now string) error {
	if pageID == "" || psid == "" || externalMessageID == "" || now == "" || len(pageID) > 128 || len(psid) > 128 || len(externalMessageID) > 256 {
		return nil
	}
	_, err := db.ExecContext(ctx, `INSERT INTO facebook_message_receipts (external_message_id,page_id,psid,received_at,message_text,status) VALUES (?,?,?,?,?,?) ON CONFLICT(external_message_id) DO UPDATE SET status=excluded.status`, externalMessageID, pageID, psid, now, text, status)
	return err
}

func ProcessFacebookMessage(ctx context.Context, db *sql.DB, pageID, psid, externalMessageID, text string, echo, hasAttachments bool, now string) (FacebookMessageOutcome, error) {
	_ = hasAttachments
	if strings.TrimSpace(pageID) == "" || strings.TrimSpace(psid) == "" || strings.TrimSpace(externalMessageID) == "" || now == "" || len(pageID) > 128 || len(psid) > 128 || len(externalMessageID) > 256 || echo || strings.TrimSpace(text) == "" {
		return FacebookMessageOutcome{Kind: FacebookMessageIgnored}, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return FacebookMessageOutcome{}, err
	}
	defer tx.Rollback()

	var configured bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM facebook_integrations WHERE page_id=? AND status='active')`, pageID).Scan(&configured); err != nil {
		return FacebookMessageOutcome{}, err
	}
	if !configured {
		return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageIgnored})
	}
	claim, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO facebook_message_receipts (external_message_id, page_id, psid, received_at, message_text, status) VALUES (?, ?, ?, ?, ?, 'ignored')`, externalMessageID, pageID, psid, now, text)
	if err != nil {
		return FacebookMessageOutcome{}, err
	}
	if count, err := claim.RowsAffected(); err != nil {
		return FacebookMessageOutcome{}, err
	} else if count == 0 {
		return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageDuplicate})
	}

	codeHash := sha256.Sum256([]byte(text))
	var identityID, userID int64
	var consumedHash []byte
	err = tx.QueryRowContext(ctx, `SELECT id, user_id, verification_code_hash FROM social_identities WHERE platform='facebook' AND status='active' AND platform_user_id=?`, psid).Scan(&identityID, &userID, &consumedHash)
	if err == nil {
		if len(consumedHash) == sha256.Size && subtle.ConstantTimeCompare(consumedHash, codeHash[:]) == 1 {
			return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageDuplicate, IdentityID: identityID})
		}
		instant, err := time.Parse(time.RFC3339Nano, now)
		if err != nil {
			return FacebookMessageOutcome{}, err
		}
		jakarta, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			return FacebookMessageOutcome{}, err
		}
		title := "Facebook Messenger " + instant.In(jakarta).Format("01/02/2006 15:04")
		result, err := tx.ExecContext(ctx, `INSERT INTO notes (user_id, social_identity_id, title, content_markdown, source, external_message_id) VALUES (?, ?, ?, ?, 'facebook', ?)`, userID, identityID, title, text, externalMessageID)
		if err != nil {
			return FacebookMessageOutcome{}, err
		}
		noteID, err := result.LastInsertId()
		if err != nil {
			return FacebookMessageOutcome{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE facebook_message_receipts SET status='note_created', note_id=? WHERE external_message_id=?`, noteID, externalMessageID); err != nil {
			return FacebookMessageOutcome{}, err
		}
		return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageNoteCreated, IdentityID: identityID})
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return FacebookMessageOutcome{}, err
	}

	var pendingID int64
	var expiresAt string
	err = tx.QueryRowContext(ctx, `SELECT id, verification_expires_at FROM social_identities WHERE platform='facebook' AND status='pending' AND verification_consumed_at IS NULL AND verification_code_hash=?`, codeHash[:]).Scan(&pendingID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageIgnored})
	}
	if err != nil {
		return FacebookMessageOutcome{}, err
	}
	if expiresAt <= now {
		if _, err := tx.ExecContext(ctx, `UPDATE social_identities SET verification_result='expired', verification_result_at=?, updated_at=? WHERE id=?`, now, now, pendingID); err != nil {
			return FacebookMessageOutcome{}, err
		}
		return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageExpired, IdentityID: pendingID})
	}
	claimReply, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO facebook_verification_replies (external_message_id, social_identity_id, result) VALUES (?, ?, 'claimed')`, externalMessageID, pendingID)
	if err != nil {
		return FacebookMessageOutcome{}, err
	}
	if count, err := claimReply.RowsAffected(); err != nil {
		return FacebookMessageOutcome{}, err
	} else if count == 0 {
		return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageDuplicate, IdentityID: pendingID})
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE social_identities SET platform_user_id=?, status='active', verified_at=?, verification_consumed_at=?, updated_at=?
		WHERE id=? AND status='pending' AND verification_consumed_at IS NULL`, psid, now, now, now, pendingID)
	if err != nil {
		if strings.Contains(err.Error(), "social_identities.platform, social_identities.platform_user_id") {
			if _, updateErr := tx.ExecContext(ctx, `UPDATE social_identities SET verification_result='invalid_code', verification_result_at=?, updated_at=? WHERE id=?`, now, now, pendingID); updateErr != nil {
				return FacebookMessageOutcome{}, updateErr
			}
			return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageInvalidCode, IdentityID: pendingID})
		}
		return FacebookMessageOutcome{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		if err != nil {
			return FacebookMessageOutcome{}, err
		}
		return FacebookMessageOutcome{}, errors.New("facebook verification activation lost pending identity")
	}
	return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageActivated, IdentityID: pendingID, OwnsReply: true})
}

func RecordFacebookVerificationReply(ctx context.Context, db *sql.DB, externalMessageID, attemptedAt string, sent bool) error {
	result := "sent"
	if !sent {
		result = "system_failure"
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	update, err := tx.ExecContext(ctx, `UPDATE facebook_verification_replies SET attempted_at=?, result=? WHERE external_message_id=? AND result='claimed'`, attemptedAt, result, externalMessageID)
	if err != nil {
		return err
	}
	if count, err := update.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return errors.New("verification reply claim unavailable")
	}
	if !sent {
		if _, err := tx.ExecContext(ctx, `UPDATE social_identities SET verification_result='system_failure', verification_result_at=?, updated_at=? WHERE id=(SELECT social_identity_id FROM facebook_verification_replies WHERE external_message_id=?)`, attemptedAt, attemptedAt, externalMessageID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func commitFacebookMessageOutcome(tx *sql.Tx, outcome FacebookMessageOutcome) (FacebookMessageOutcome, error) {
	if err := tx.Commit(); err != nil {
		return FacebookMessageOutcome{}, err
	}
	return outcome, nil
}
