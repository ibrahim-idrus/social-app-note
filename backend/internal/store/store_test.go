package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenAppliesFoundationMigrationOnce(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range []string{"users", "social_identities", "notes", "sessions", "instagram_integrations", "facebook_integrations", "facebook_message_receipts", "facebook_verification_replies"} {
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
	if count != 15 {
		t.Fatalf("migration count = %d, want 15", count)
	}
}

func TestMessageNoteFields(t *testing.T) {
	tests := []struct {
		name, text  string
		urls        []string
		title, body string
	}{
		{"text", "Save this #Idea", nil, "Save this #Idea", "Save this #Idea"},
		{"url first", "Save this #Idea", []string{"https://example.com/post"}, "Save this #Idea", "https://example.com/post\nSave this #Idea"},
		{"no duplicate", "See https://example.com/post #Idea", []string{"https://example.com/post"}, "See https://example.com/post #Idea", "See https://example.com/post #Idea"},
		{"attachment only", "", []string{"https://example.com/post"}, "Shared post", "https://example.com/post"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, body := messageNoteFields(tt.text, tt.urls)
			if title != tt.title || body != tt.body {
				t.Fatalf("got %q / %q", title, body)
			}
		})
	}
	long := strings.Repeat("é", 250)
	title, body := messageNoteFields(long, nil)
	if len([]rune(title)) != 200 || body != long {
		t.Fatalf("long title=%d body preserved=%v", len([]rune(title)), body == long)
	}
}

func TestNoteTagsAreReanalyzedOnUpdate(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'A','a@b.c','x')`); err != nil {
		t.Fatal(err)
	}
	note, err := CreateNote(context.Background(), db, 1, "Tagged", "#Idea #café #IDEA punctuation.#no")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(note.Tags, "|") != "café|Idea|no" {
		t.Fatalf("tags=%#v", note.Tags)
	}
	note, err = UpdateNote(context.Background(), db, 1, note.ID, "Changed", "now #Work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(note.Tags, "|") != "Work" {
		t.Fatalf("updated tags=%#v", note.Tags)
	}
}

func TestListNotesFiltersSourcesAndTagsAndListsAvailableTags(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'A','a@b.c','x'),(2,'B','b@b.c','x')`); err != nil {
		t.Fatal(err)
	}
	for _, note := range []struct {
		user        int64
		title, body string
	}{{1, "one", "#Red #Blue"}, {1, "two", "#Blue"}, {1, "three", "#Green"}, {2, "private", "#Red"}} {
		if _, err := CreateNote(context.Background(), db, note.user, note.title, note.body); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListNotesFiltered(context.Background(), db, 1, "", "all", []string{"manual", "instagram"}, []string{"red", "green"}, "updated_at", "desc", 1, 20)
	if err != nil || got.Total != 2 {
		t.Fatalf("filtered=%#v err=%v", got, err)
	}
	tags, err := ListTags(context.Background(), db, 1)
	if err != nil || strings.Join(tags, "|") != "Blue|Green|Red" {
		t.Fatalf("tags=%#v err=%v", tags, err)
	}
}

func TestMigrateRepairsInstagramNoteEventsForeignKey(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		INSERT INTO users (id, name, email, password_hash) VALUES (1, 'Test', 'test@example.com', 'hash');
		INSERT INTO notes (id, user_id, title, content_markdown, source) VALUES (1, 1, 'title', 'body', 'instagram');
		PRAGMA foreign_keys=OFF;
		ALTER TABLE instagram_note_events RENAME TO instagram_note_events_current;
		CREATE TABLE instagram_note_events (external_message_id TEXT PRIMARY KEY, note_id INTEGER NOT NULL REFERENCES notes_before_facebook(id) ON DELETE CASCADE);
		INSERT INTO instagram_note_events VALUES ('event-1', 1);
		DROP TABLE instagram_note_events_current;
		DELETE FROM schema_migrations WHERE name='0010_repair_instagram_note_events.sql';
		PRAGMA foreign_keys=ON;
	`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var target string
	if err := db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('instagram_note_events') WHERE "from"='note_id'`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if target != "notes" {
		t.Fatalf("note_id references %q, want notes", target)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM instagram_note_events WHERE external_message_id='event-1' AND note_id=1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preserved event count = %d, err %v", count, err)
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

func TestListNotesSearchTreatsSpecialCharactersLiterallyAndTotalsNotes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users (id, name, email, password_hash) VALUES (1, 'Alice', 'alice@example.com', 'hash'), (2, 'Bob', 'bob@example.com', 'hash')`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		user           int
		title, content string
	}{{1, `% literal`, "percent"}, {1, `_ literal`, "underscore"}, {1, `back\slash`, "slash"}, {1, `Mixed CASE`, "quotes ' work"}, {1, `Unicode 東京`, "unicode"}, {2, `% literal private`, "percent"}} {
		if _, err := db.Exec(`INSERT INTO notes (user_id, title, content_markdown, source) VALUES (?, ?, ?, 'manual')`, row.user, row.title, row.content); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"%", "_", `\`, "mixed case", "'", "東京"} {
		result, err := ListNotes(context.Background(), db, 1, query, "all", "", "updated_at", "desc", 1, 1)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		if result.Total != 1 || len(result.Notes) != 1 {
			t.Fatalf("query %q returned total=%d notes=%d", query, result.Total, len(result.Notes))
		}
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
			if test.want == InstagramDMUnmatched {
				var notes int
				if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil || notes != 0 {
					t.Fatalf("unmatched message created %d notes: %v", notes, err)
				}
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

func TestInstagramAttachmentSplitCollectionAndReplay(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'A','a@b.c','x'); INSERT INTO instagram_integrations(instagram_user_id,username,access_token,owner_user_id,status) VALUES('inbox','inbox','x',1,'active'); INSERT INTO social_identities(id,user_id,platform,platform_user_id,username,normalized_username,status) VALUES(1,1,'instagram','sender','a','a','active')`); err != nil {
		t.Fatal(err)
	}
	a := []InstagramAttachment{{Type: "ig_post", URL: "https://lookaside.fbsbx.com/media.webp", InstagramMediaID: "media-1", Alt: "Instagram post"}}
	if _, err := ProcessInstagramMessage(context.Background(), db, "inbox", "sender", "attachment-mid", "", a, "2026-09-29T08:19:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessInstagramMessage(context.Background(), db, "inbox", "sender", "text-mid", "follow-up", nil, "2026-09-29T08:19:01Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessInstagramMessage(context.Background(), db, "inbox", "sender", "text-mid", "replay", nil, "2026-09-29T08:19:02Z"); err != nil {
		t.Fatal(err)
	}
	var count int
	var title, content string
	var raw sql.NullString
	if err := db.QueryRow(`SELECT count(*), title, content_markdown, instagram_attachments_json FROM notes`).Scan(&count, &title, &content, &raw); err != nil {
		t.Fatal(err)
	}
	if count != 1 || title != "follow-up" || content != "https://lookaside.fbsbx.com/media.webp\nfollow-up" || !raw.Valid || !strings.Contains(raw.String, `"type":"ig_post"`) || !strings.Contains(raw.String, `"url":"https://lookaside.fbsbx.com/media.webp"`) || !strings.Contains(raw.String, `"media_id":"media-1"`) {
		t.Fatalf("note=%d %q %q %q", count, title, content, raw.String)
	}
}

func TestInstagramAttachmentsRequireVerifiedSender(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'A','a@b.c','x'); INSERT INTO instagram_integrations(instagram_user_id,username,access_token,owner_user_id,status) VALUES('inbox','inbox','x',1,'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessInstagramMessage(context.Background(), db, "inbox", "unknown", "mid", "", []InstagramAttachment{{Type: "ig_post", URL: "https://example.com/a.webp"}}, "2026-09-29T08:19:00Z"); err != nil {
		t.Fatal(err)
	}
	var notes int
	_ = db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes)
	if notes != 0 {
		t.Fatalf("notes=%d", notes)
	}
}

func TestMigrationBackfillsTagsFromExistingNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlite.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'A','a@b.c','x'); INSERT INTO notes(user_id,title,content_markdown,source) VALUES(1,'old','before #Legacy','manual'); DELETE FROM note_tags; DELETE FROM schema_migrations WHERE name='0013_backfill_note_tags.sql'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM note_tags nt JOIN tags t ON t.id=nt.tag_id WHERE t.normalized_name='legacy'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("backfill count=%d err=%v", count, err)
	}
}
