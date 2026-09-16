package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenAppliesFoundationMigrationOnce(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range []string{"users", "social_identities", "notes", "sessions"} {
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
	if count != 2 {
		t.Fatalf("migration count = %d, want 2", count)
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
