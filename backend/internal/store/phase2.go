package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrPlatformIdentityAlreadyRegistered = errors.New("platform identity already registered")
	ErrIdentityUnavailable               = errors.New("identity unavailable")
)

type SocialIdentity struct {
	ID                 int64   `json:"id"`
	UserID             int64   `json:"-"`
	Platform           string  `json:"platform"`
	PlatformUserID     *string `json:"platform_user_id"`
	Username           string  `json:"username"`
	NormalizedUsername string  `json:"normalized_username"`
	DisplayName        *string `json:"display_name"`
	AvatarURL          *string `json:"avatar_url"`
	Status             string  `json:"status"`
	VerifiedAt         *string `json:"verified_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func ListSocialIdentities(ctx context.Context, db *sql.DB, userID int64) ([]SocialIdentity, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, platform, platform_user_id, username, normalized_username,
			display_name, avatar_url, status, verified_at, created_at, updated_at
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
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func CreatePendingSocialIdentity(ctx context.Context, db *sql.DB, userID int64, platform, username string) (SocialIdentity, error) {
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
		INSERT INTO social_identities (user_id, platform, username, normalized_username, status)
		VALUES (?, ?, ?, ?, 'pending')`, userID, platform, username, username)
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
			display_name, avatar_url, status, verified_at, created_at, updated_at
		FROM social_identities WHERE id = ? AND user_id = ?`, id, userID)
	err := scanSocialIdentity(row, &identity)
	return identity, err
}

type identityScanner interface {
	Scan(...any) error
}

func scanSocialIdentity(scanner identityScanner, identity *SocialIdentity) error {
	return scanner.Scan(
		&identity.ID, &identity.UserID, &identity.Platform, &identity.PlatformUserID,
		&identity.Username, &identity.NormalizedUsername, &identity.DisplayName,
		&identity.AvatarURL, &identity.Status, &identity.VerifiedAt,
		&identity.CreatedAt, &identity.UpdatedAt,
	)
}
