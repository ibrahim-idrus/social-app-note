package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrPlatformIdentityAlreadyRegistered = errors.New("platform identity already registered")
	ErrIdentityUnavailable               = errors.New("identity unavailable")
)

type SocialIdentity struct {
	ID                 int64                 `json:"id"`
	UserID             int64                 `json:"-"`
	Platform           string                `json:"platform"`
	PlatformUserID     *string               `json:"platform_user_id"`
	Username           string                `json:"username"`
	NormalizedUsername string                `json:"normalized_username"`
	DisplayName        *string               `json:"display_name"`
	AvatarURL          *string               `json:"avatar_url"`
	Status             string                `json:"status"`
	VerifiedAt         *string               `json:"verified_at"`
	VerificationCode   string                `json:"verification_code,omitempty"`
	VerificationExpiry *string               `json:"verification_expires_at,omitempty"`
	InstagramAccount   *InstagramIntegration `json:"instagram_account,omitempty"`
	CreatedAt          string                `json:"created_at"`
	UpdatedAt          string                `json:"updated_at"`
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
			display_name, avatar_url, status, verified_at, verification_expires_at, created_at, updated_at
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
		identity.InstagramAccount = &integration
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func CreatePendingSocialIdentity(ctx context.Context, db *sql.DB, userID int64, platform, username string, codeHash []byte, expiresAt string) (SocialIdentity, error) {
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
			user_id, platform, username, normalized_username, status,
			verification_code_hash, verification_expires_at
		) VALUES (?, ?, ?, ?, 'pending', ?, ?)`, userID, platform, username, username, codeHash, expiresAt)
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
			display_name, avatar_url, status, verified_at, verification_expires_at, created_at, updated_at
		FROM social_identities WHERE id = ? AND user_id = ?`, id, userID)
	err := scanSocialIdentity(row, &identity)
	if err == nil {
		integration, integrationErr := instagramIntegration(ctx, db)
		if integrationErr != nil {
			return SocialIdentity{}, integrationErr
		}
		identity.InstagramAccount = &integration
	}
	return identity, err
}

func instagramIntegration(ctx context.Context, db *sql.DB) (InstagramIntegration, error) {
	var integration InstagramIntegration
	err := db.QueryRowContext(ctx, `
		SELECT id, instagram_user_id, username, access_token, status, created_at, updated_at
		FROM instagram_integrations WHERE id = 'prototype'`).Scan(
		&integration.ID, &integration.InstagramUserID, &integration.Username, &integration.AccessToken,
		&integration.Status, &integration.CreatedAt, &integration.UpdatedAt,
	)
	return integration, err
}

func ProcessInstagramDM(ctx context.Context, db *sql.DB, recipientID, senderID, externalMessageID, text, now string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var recipientExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM instagram_integrations
		WHERE instagram_user_id = ? AND status = 'active'
	)`, recipientID).Scan(&recipientExists); err != nil {
		return err
	}
	if !recipientExists {
		return tx.Commit()
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
			return tx.Commit()
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO notes (
				user_id, social_identity_id, title, content_markdown, source, external_message_id
			) VALUES (?, ?, 'Instagram DM', ?, 'instagram', ?)`, userID, identityID, text, externalMessageID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE social_identities
		SET platform_user_id = ?, status = 'active', verified_at = ?, verification_consumed_at = ?, updated_at = ?
		WHERE platform = 'instagram' AND status = 'pending'
			AND verification_code_hash = ? AND verification_expires_at > ?
			AND verification_consumed_at IS NULL`, senderID, now, now, now, codeHash[:], now)
	if err != nil {
		if strings.Contains(err.Error(), "social_identities.platform, social_identities.platform_user_id") {
			return tx.Commit()
		}
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count > 1 {
		return errors.New("verification code matched multiple identities")
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
		&identity.CreatedAt, &identity.UpdatedAt,
	)
}
