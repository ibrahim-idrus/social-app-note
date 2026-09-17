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
	if count != 3 {
		t.Fatalf("migration count = %d, want 3", count)
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
	for _, name := range []string{"verification_code_hash", "verification_expires_at", "verification_consumed_at"} {
		if !columns[name] {
			t.Fatalf("missing social_identities.%s", name)
		}
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
