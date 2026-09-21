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

var (
	ErrPlatformIdentityAlreadyRegistered = errors.New("platform identity already registered")
	ErrIdentityUnavailable               = errors.New("identity unavailable")
)

type InstagramDMOutcomeKind string

const (
	InstagramDMUnmatched   InstagramDMOutcomeKind = "unmatched"
	InstagramDMDuplicate   InstagramDMOutcomeKind = "duplicate"
	InstagramDMNoteCreated InstagramDMOutcomeKind = "note_created"
	InstagramDMActivated   InstagramDMOutcomeKind = "activated"
	InstagramDMInvalidCode InstagramDMOutcomeKind = "invalid_code"
	InstagramDMExpired     InstagramDMOutcomeKind = "expired"
)

type InstagramDMOutcome struct {
	Kind       InstagramDMOutcomeKind
	IdentityID int64
	Username   string
	OwnsReply  bool
}

type SocialIdentity struct {
	ID                    int64                 `json:"id"`
	UserID                int64                 `json:"-"`
	Platform              string                `json:"platform"`
	PlatformUserID        *string               `json:"platform_user_id"`
	Username              string                `json:"username"`
	NormalizedUsername    string                `json:"normalized_username"`
	DisplayName           *string               `json:"display_name"`
	AvatarURL             *string               `json:"avatar_url"`
	Status                string                `json:"status"`
	VerifiedAt            *string               `json:"verified_at"`
	VerificationState     string                `json:"verification_state"`
	VerificationResult    string                `json:"-"`
	VerificationUpdatedAt *string               `json:"verification_updated_at"`
	VerificationCode      string                `json:"verification_code,omitempty"`
	VerificationExpiry    *string               `json:"verification_expires_at,omitempty"`
	InstagramAccount      *InstagramIntegration `json:"instagram_account,omitempty"`
	CreatedAt             string                `json:"created_at"`
	UpdatedAt             string                `json:"updated_at"`
}

type InstagramIntegration struct {
	ID              string  `json:"-"`
	InstagramUserID string  `json:"instagram_user_id"`
	Username        string  `json:"username"`
	AccessToken     *string `json:"-"`
	Status          string  `json:"-"`
	CreatedAt       string  `json:"-"`
	UpdatedAt       string  `json:"-"`
}

func ListSocialIdentities(ctx context.Context, db *sql.DB, userID int64) ([]SocialIdentity, error) {
	integration, err := instagramIntegration(ctx, db)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, platform, platform_user_id, username, normalized_username,
			display_name, avatar_url, status, verified_at, verification_expires_at,
			verification_result, verification_result_at, created_at, updated_at
		FROM social_identities WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	identities := []SocialIdentity{}
	for rows.Next() {
		var identity SocialIdentity
		if err := scanSocialIdentity(rows, &identity); err != nil {
			return nil, err
		}
		setVerificationState(&identity, time.Now().UTC())
		identity.InstagramAccount = &integration
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func CreatePendingSocialIdentity(ctx context.Context, db *sql.DB, userID int64, platform, platformUserID, username, displayName, avatarURL string, codeHash []byte, expiresAt string) (SocialIdentity, error) {
	var stableID any
	if platformUserID != "" {
		stableID = platformUserID
	}
	var occupied bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM social_identities WHERE user_id = ? AND platform = ?
	)`, userID, platform).Scan(&occupied); err != nil {
		return SocialIdentity{}, err
	}
	if occupied {
		return SocialIdentity{}, ErrPlatformIdentityAlreadyRegistered
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO social_identities (
			user_id, platform, platform_user_id, username, normalized_username, display_name, avatar_url, status,
			verification_code_hash, verification_expires_at, verification_result, verification_result_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, 'waiting', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, userID, platform, stableID, username, username, displayName, avatarURL, codeHash, expiresAt)
	if err != nil {
		message := err.Error()
		if strings.Contains(message, "social_identities.user_id, social_identities.platform") {
			return SocialIdentity{}, ErrPlatformIdentityAlreadyRegistered
		}
		if strings.Contains(message, "social_identities.platform, social_identities.normalized_username") ||
			strings.Contains(message, "social_identities.platform, social_identities.platform_user_id") {
			return SocialIdentity{}, ErrIdentityUnavailable
		}
		return SocialIdentity{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SocialIdentity{}, err
	}
	return socialIdentityByID(ctx, db, userID, id)
}

func ReplaceSocialIdentityVerification(ctx context.Context, db *sql.DB, userID, id int64, codeHash []byte, expiresAt string) (SocialIdentity, error) {
	result, err := db.ExecContext(ctx, `
		UPDATE social_identities
		SET verification_code_hash = ?, verification_expires_at = ?, verification_consumed_at = NULL,
			verification_result = 'waiting', verification_result_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND user_id = ? AND platform = 'instagram' AND status = 'pending'`, codeHash, expiresAt, id, userID)
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

func DeleteSocialIdentity(ctx context.Context, db *sql.DB, userID, id int64) error {
	result, err := db.ExecContext(ctx, `DELETE FROM social_identities WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func socialIdentityByID(ctx context.Context, db *sql.DB, userID, id int64) (SocialIdentity, error) {
	var identity SocialIdentity
	row := db.QueryRowContext(ctx, `
		SELECT id, user_id, platform, platform_user_id, username, normalized_username,
			display_name, avatar_url, status, verified_at, verification_expires_at,
			verification_result, verification_result_at, created_at, updated_at
		FROM social_identities WHERE id = ? AND user_id = ?`, id, userID)
	err := scanSocialIdentity(row, &identity)
	if err == nil {
		setVerificationState(&identity, time.Now().UTC())
		integration, integrationErr := instagramIntegration(ctx, db)
		if integrationErr != nil {
			return SocialIdentity{}, integrationErr
		}
		identity.InstagramAccount = &integration
	}
	return identity, err
}

func GetInstagramIntegration(ctx context.Context, db *sql.DB) (InstagramIntegration, error) {
	var integration InstagramIntegration
	err := db.QueryRowContext(ctx, `
		SELECT id, instagram_user_id, username, access_token, status, created_at, updated_at
		FROM instagram_integrations WHERE id = 'prototype'`).Scan(
		&integration.ID, &integration.InstagramUserID, &integration.Username, &integration.AccessToken,
		&integration.Status, &integration.CreatedAt, &integration.UpdatedAt,
	)
	return integration, err
}

func instagramIntegration(ctx context.Context, db *sql.DB) (InstagramIntegration, error) {
	return GetInstagramIntegration(ctx, db)
}

func ConfigureInstagramIntegration(ctx context.Context, db *sql.DB, accountID, username, accessToken string) error {
	if accountID == "" || username == "" {
		return nil
	}
	var token any
	if accessToken != "" {
		token = accessToken
	}
	_, err := db.ExecContext(ctx, `UPDATE instagram_integrations SET instagram_user_id=?, username=?, access_token=?, status='active', updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id='prototype'`, accountID, username, token)
	return err
}

func ConfigureInstagramIntegrationOwner(ctx context.Context, db *sql.DB, accountID, ownerEmail string) error {
	if accountID == "" || ownerEmail == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `UPDATE instagram_integrations SET owner_user_id=(SELECT id FROM users WHERE lower(email)=lower(?)) WHERE instagram_user_id=?`, ownerEmail, accountID)
	return err
}

func InstagramIntegrationOwnerID(ctx context.Context, db *sql.DB, accountID string) (int64, error) {
	var ownerID int64
	err := db.QueryRowContext(ctx, `SELECT owner_user_id FROM instagram_integrations WHERE instagram_user_id=? AND status='active'`, accountID).Scan(&ownerID)
	return ownerID, err
}

func ProcessInstagramDM(ctx context.Context, db *sql.DB, recipientID, senderID, externalMessageID, text, now string) (InstagramDMOutcome, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return InstagramDMOutcome{}, err
	}
	defer tx.Rollback()

	var recipientExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM instagram_integrations
		WHERE instagram_user_id = ? AND status = 'active'
	)`, recipientID).Scan(&recipientExists); err != nil {
		return InstagramDMOutcome{}, err
	}
	if !recipientExists {
		return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMUnmatched})
	}

	codeHash := sha256.Sum256([]byte(text))
	var identityID, userID int64
	var consumedHash []byte
	err = tx.QueryRowContext(ctx, `
		SELECT id, user_id, verification_code_hash
		FROM social_identities
		WHERE platform = 'instagram' AND status = 'active' AND platform_user_id = ?`, senderID).Scan(
		&identityID, &userID, &consumedHash,
	)
	if err == nil {
		if len(consumedHash) == sha256.Size && subtle.ConstantTimeCompare(consumedHash, codeHash[:]) == 1 {
			return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMDuplicate, IdentityID: identityID})
		}
		result, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO notes (
				user_id, social_identity_id, title, content_markdown, source, external_message_id
			) VALUES (?, ?, 'Instagram DM', ?, 'instagram', ?)`, userID, identityID, text, externalMessageID)
		if err != nil {
			return InstagramDMOutcome{}, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return InstagramDMOutcome{}, err
		}
		kind := InstagramDMNoteCreated
		if count == 0 {
			kind = InstagramDMDuplicate
		}
		return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: kind, IdentityID: identityID})
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return InstagramDMOutcome{}, err
	}

	var pendingID int64
	var username, expiresAt string
	var consumedAt sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id, username, verification_expires_at, verification_consumed_at
		FROM social_identities
		WHERE platform = 'instagram' AND status = 'pending' AND verification_code_hash = ?`, codeHash[:]).Scan(
		&pendingID, &username, &expiresAt, &consumedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMUnmatched})
	}
	if err != nil {
		return InstagramDMOutcome{}, err
	}
	if consumedAt.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE social_identities SET verification_result = 'invalid_code', verification_result_at = ?, updated_at = ? WHERE id = ?`, now, now, pendingID); err != nil {
			return InstagramDMOutcome{}, err
		}
		return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMInvalidCode, IdentityID: pendingID, Username: username})
	}
	if expiresAt <= now {
		if _, err := tx.ExecContext(ctx, `UPDATE social_identities SET verification_result = 'expired', verification_result_at = ?, updated_at = ? WHERE id = ?`, now, now, pendingID); err != nil {
			return InstagramDMOutcome{}, err
		}
		return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMExpired, IdentityID: pendingID, Username: username})
	}

	claim, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO instagram_verification_replies (external_message_id, social_identity_id, result) VALUES (?, ?, 'claimed')`, externalMessageID, pendingID)
	if err != nil {
		return InstagramDMOutcome{}, err
	}
	if count, err := claim.RowsAffected(); err != nil {
		return InstagramDMOutcome{}, err
	} else if count == 0 {
		return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMDuplicate, IdentityID: pendingID, Username: username})
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE social_identities
		SET platform_user_id = ?, status = 'active', verified_at = ?, verification_consumed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'pending' AND verification_consumed_at IS NULL`, senderID, now, now, now, pendingID)
	if err != nil {
		if strings.Contains(err.Error(), "social_identities.platform, social_identities.platform_user_id") {
			if _, deleteErr := tx.ExecContext(ctx, `DELETE FROM instagram_verification_replies WHERE external_message_id = ?`, externalMessageID); deleteErr != nil {
				return InstagramDMOutcome{}, deleteErr
			}
			if _, updateErr := tx.ExecContext(ctx, `UPDATE social_identities SET verification_result = 'invalid_code', verification_result_at = ?, updated_at = ? WHERE id = ?`, now, now, pendingID); updateErr != nil {
				return InstagramDMOutcome{}, updateErr
			}
			return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMInvalidCode, IdentityID: pendingID, Username: username})
		}
		return InstagramDMOutcome{}, err
	}
	if count, err := result.RowsAffected(); err != nil {
		return InstagramDMOutcome{}, err
	} else if count != 1 {
		return InstagramDMOutcome{}, errors.New("verification activation lost pending identity")
	}
	return commitInstagramDMOutcome(tx, InstagramDMOutcome{Kind: InstagramDMActivated, IdentityID: pendingID, Username: username, OwnsReply: true})
}

func commitInstagramDMOutcome(tx *sql.Tx, outcome InstagramDMOutcome) (InstagramDMOutcome, error) {
	if err := tx.Commit(); err != nil {
		return InstagramDMOutcome{}, err
	}
	return outcome, nil
}

// SaveInstagramInboxNote stores an inbox DM that no identity claimed (e.g. a
// stranger's message) under the inbox owner's account. Dedup is free via the
// UNIQUE(source, external_message_id) constraint: re-polls are no-ops.
func SaveInstagramInboxNote(ctx context.Context, db *sql.DB, ownerID int64, senderLabel, messageID, text string) (bool, error) {
	if strings.TrimSpace(text) == "" || messageID == "" {
		return false, nil
	}
	if len(text) > 100_000 {
		text = text[:100_000]
	}
	result, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO notes (user_id, title, content_markdown, source, external_message_id) VALUES (?, ?, ?, 'instagram', ?)`,
		ownerID, "Instagram DM from @"+senderLabel, text, messageID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func RecordInstagramVerificationReply(ctx context.Context, db *sql.DB, externalMessageID, attemptedAt string, sent bool) error {
	result := "sent"
	if !sent {
		result = "system_failure"
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	update, err := tx.ExecContext(ctx, `
		UPDATE instagram_verification_replies
		SET attempted_at = ?, result = ?
		WHERE external_message_id = ? AND result = 'claimed'`, attemptedAt, result, externalMessageID)
	if err != nil {
		return err
	}
	count, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("verification reply claim unavailable")
	}
	if !sent {
		if _, err := tx.ExecContext(ctx, `
			UPDATE social_identities
			SET verification_result = 'system_failure', verification_result_at = ?, updated_at = ?
			WHERE id = (SELECT social_identity_id FROM instagram_verification_replies WHERE external_message_id = ?)`, attemptedAt, attemptedAt, externalMessageID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type identityScanner interface {
	Scan(...any) error
}

func scanSocialIdentity(scanner identityScanner, identity *SocialIdentity) error {
	return scanner.Scan(
		&identity.ID, &identity.UserID, &identity.Platform, &identity.PlatformUserID,
		&identity.Username, &identity.NormalizedUsername, &identity.DisplayName,
		&identity.AvatarURL, &identity.Status, &identity.VerifiedAt, &identity.VerificationExpiry,
		&identity.VerificationResult, &identity.VerificationUpdatedAt,
		&identity.CreatedAt, &identity.UpdatedAt,
	)
}

func setVerificationState(identity *SocialIdentity, now time.Time) {
	identity.VerificationState = identity.VerificationResult
	if identity.Status == "active" && identity.VerificationResult != "system_failure" {
		identity.VerificationState = "active"
		identity.VerificationUpdatedAt = identity.VerifiedAt
		return
	}
	if identity.Status == "pending" && identity.VerificationExpiry != nil {
		if expiresAt, err := time.Parse(time.RFC3339Nano, *identity.VerificationExpiry); err == nil && !expiresAt.After(now) {
			identity.VerificationState = "expired"
			identity.VerificationUpdatedAt = identity.VerificationExpiry
		}
	}
}
