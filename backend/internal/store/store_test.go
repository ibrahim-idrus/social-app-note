package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAppliesFoundationMigrationOnce(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range []string{"users", "social_identities", "notes", "sessions", "instagram_integrations"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("table %q count = %d, want 1", table, count)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("migration count = %d, want 4", count)
	}
}

func TestInstagramPrototypeSchemaAndBootstrap(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var integrationID, instagramUserID, username, status string
	var accessToken sql.NullString
	if err := db.QueryRow(`
		SELECT id, instagram_user_id, username, access_token, status
		FROM instagram_integrations`).Scan(&integrationID, &instagramUserID, &username, &accessToken, &status); err != nil {
		t.Fatal(err)
	}
	if integrationID != "prototype" || instagramUserID == "" || username == "" || accessToken.Valid || status != "active" {
		t.Fatalf("prototype integration = %q %q %q %#v %q", integrationID, instagramUserID, username, accessToken, status)
	}

	columns := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(social_identities)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"verification_code_hash", "verification_expires_at", "verification_consumed_at", "verification_result", "verification_result_at"} {
		if !columns[name] {
			t.Fatalf("missing social_identities.%s", name)
		}
	}

	if _, err := db.Exec(`INSERT INTO instagram_verification_replies (external_message_id, social_identity_id, result) VALUES ('event-1', 1, 'claimed')`); err == nil {
		t.Fatal("reply claim accepted a missing identity")
	}
	if _, err := db.Exec(`INSERT INTO instagram_verification_replies (external_message_id, result) VALUES ('event-1', 'unknown')`); err == nil {
		t.Fatal("reply claim accepted an invalid result")
	}
}

func TestFoundationConstraints(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO users (id, name, email, password_hash) VALUES (1, 'Test User', 'test@example.com', 'hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notes (user_id, title, content_markdown, source) VALUES (1, 'title', 'body', 'email')`); err == nil {
		t.Fatal("invalid note source was accepted")
	}
}

func TestSocialIdentityDatabaseConstraints(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range []string{
		`INSERT INTO users (id, name, email, password_hash) VALUES (1, 'Alice', 'alice@example.com', 'hash')`,
		`INSERT INTO users (id, name, email, password_hash) VALUES (2, 'Bob', 'bob@example.com', 'hash')`,
		`INSERT INTO social_identities (user_id, platform, platform_user_id, username, normalized_username, status) VALUES (1, 'instagram', 'stable-1', 'Alice', 'alice', 'active')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for name, statement := range map[string]string{
		"one slot per user/platform": `INSERT INTO social_identities (user_id, platform, username, normalized_username, status) VALUES (1, 'instagram', 'Other', 'other', 'pending')`,
		"global normalized username": `INSERT INTO social_identities (user_id, platform, username, normalized_username, status) VALUES (2, 'instagram', 'ALICE', 'alice', 'pending')`,
		"global stable ID":           `INSERT INTO social_identities (user_id, platform, platform_user_id, username, normalized_username, status) VALUES (2, 'instagram', 'stable-1', 'Bob', 'bob', 'active')`,
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("%s constraint accepted duplicate", name)
		}
	}

	if _, err := db.Exec(`INSERT INTO notes (id, user_id, social_identity_id, title, content_markdown, source) VALUES (1, 1, 1, 'DM', 'body', 'instagram')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM social_identities WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	var identityID sql.NullInt64
	if err := db.QueryRow(`SELECT social_identity_id FROM notes WHERE id = 1`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if identityID.Valid {
		t.Fatalf("social_identity_id = %d, want NULL", identityID.Int64)
	}
}

func TestProcessInstagramDMReturnsVerificationOutcomesAndClaimsReplyAtomically(t *testing.T) {
	newPending := func(t *testing.T, consumed bool, expiresAt string) (*sql.DB, string) {
		t.Helper()
		db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		code := "verification-code"
		hash := sha256.Sum256([]byte(code))
		if _, err := db.Exec(`INSERT INTO users (id, name, email, password_hash) VALUES (1, 'Alice', 'alice@example.com', 'hash')`); err != nil {
			t.Fatal(err)
		}
		var consumedAt any
		if consumed {
			consumedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if _, err := db.Exec(`
			INSERT INTO social_identities (
				id, user_id, platform, username, normalized_username, status,
				verification_code_hash, verification_expires_at, verification_consumed_at
			) VALUES (1, 1, 'instagram', 'alice', 'alice', 'pending', ?, ?, ?)`, hash[:], expiresAt, consumedAt); err != nil {
			t.Fatal(err)
		}
		return db, code
	}
	now := time.Now().UTC()

	t.Run("activation and duplicate", func(t *testing.T) {
		db, code := newPending(t, false, now.Add(time.Minute).Format(time.RFC3339Nano))
		outcome, err := ProcessInstagramDM(context.Background(), db, "17841400000000000", "sender-1", "event-1", code, now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Kind != InstagramDMActivated || !outcome.OwnsReply || outcome.IdentityID != 1 || outcome.Username != "alice" {
			t.Fatalf("activation outcome = %#v", outcome)
		}
		duplicate, err := ProcessInstagramDM(context.Background(), db, "17841400000000000", "sender-1", "event-1", code, now.Format(time.RFC3339Nano))
		if err != nil || duplicate.Kind != InstagramDMDuplicate || duplicate.OwnsReply {
			t.Fatalf("duplicate outcome = %#v, err %v", duplicate, err)
		}
		var status string
		var claims, notes int
		if err := db.QueryRow(`SELECT status FROM social_identities WHERE id = 1`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM instagram_verification_replies`).Scan(&claims); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil {
			t.Fatal(err)
		}
		if status != "active" || claims != 1 || notes != 0 {
			t.Fatalf("activation state = status %q, claims %d, notes %d", status, claims, notes)
		}
	})

	for _, test := range []struct {
		name, text, expiry string
		consumed           bool
		want               InstagramDMOutcomeKind
		wantFeedback       string
	}{
		{"expired", "verification-code", now.Add(-time.Minute).Format(time.RFC3339Nano), false, InstagramDMExpired, "expired"},
		{"safely attributable invalid", "verification-code", now.Add(time.Minute).Format(time.RFC3339Nano), true, InstagramDMInvalidCode, "invalid_code"},
		{"unmatched code", "wrong-code", now.Add(time.Minute).Format(time.RFC3339Nano), false, InstagramDMUnmatched, "waiting"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _ := newPending(t, test.consumed, test.expiry)
			outcome, err := ProcessInstagramDM(context.Background(), db, "17841400000000000", "sender-1", "event-1", test.text, now.Format(time.RFC3339Nano))
			if err != nil || outcome.Kind != test.want || outcome.OwnsReply {
				t.Fatalf("outcome = %#v, err %v", outcome, err)
			}
			var feedback string
			if err := db.QueryRow(`SELECT verification_result FROM social_identities WHERE id = 1`).Scan(&feedback); err != nil {
				t.Fatal(err)
			}
			if feedback != test.wantFeedback {
				t.Fatalf("verification_result = %q, want %q", feedback, test.wantFeedback)
			}
		})
	}
}
