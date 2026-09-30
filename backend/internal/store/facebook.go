package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
)

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
	claim, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO facebook_message_receipts (external_message_id, page_id, psid, received_at) VALUES (?, ?, ?, ?)`, externalMessageID, pageID, psid, now)
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO notes (user_id, social_identity_id, title, content_markdown, source, external_message_id) VALUES (?, ?, 'Facebook Messenger', ?, 'facebook', ?)`, userID, identityID, text, externalMessageID); err != nil {
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
	return commitFacebookMessageOutcome(tx, FacebookMessageOutcome{Kind: FacebookMessageActivated, IdentityID: pendingID})
}

func commitFacebookMessageOutcome(tx *sql.Tx, outcome FacebookMessageOutcome) (FacebookMessageOutcome, error) {
	if err := tx.Commit(); err != nil {
		return FacebookMessageOutcome{}, err
	}
	return outcome, nil
}
