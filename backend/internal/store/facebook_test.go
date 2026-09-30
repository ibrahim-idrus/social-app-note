package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newFacebookStoreTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`INSERT INTO users (id, name, email, password_hash) VALUES (1, 'Alice', 'alice@example.com', 'hash')`,
		`INSERT INTO users (id, name, email, password_hash) VALUES (2, 'Bob', 'bob@example.com', 'hash')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := ConfigureFacebookIntegration(context.Background(), db, "page-1"); err != nil {
		t.Fatal(err)
	}
	return db
}

func pendingFacebookIdentity(t *testing.T, db *sql.DB, userID int64, code string, expires time.Time) int64 {
	t.Helper()
	hash := sha256.Sum256([]byte(code))
	identity, err := CreatePendingFacebookIdentity(context.Background(), db, userID, "facebook-user", hash[:], expires.UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	return identity.ID
}

func TestListFacebookMessagesIncludesIgnoredAndScopesByConnectedSender(t *testing.T) {
	db := newFacebookStoreTest(t)
	for _, statement := range []string{
		`INSERT INTO social_identities (user_id, platform, platform_user_id, username, normalized_username, status) VALUES (1, 'facebook', 'sender-1', '', '', 'active')`,
		`INSERT INTO users (id, name, email, password_hash) VALUES (3, 'Other', 'facebook-history-other@example.com', 'hash')`,
		`INSERT INTO social_identities (user_id, platform, platform_user_id, username, normalized_username, status) VALUES (3, 'facebook', 'sender-2', '', '', 'active')`,
		`INSERT INTO facebook_message_receipts (external_message_id, page_id, psid, received_at, message_text, status) VALUES ('ignored-1', 'page-1', 'sender-1', '2026-09-30T13:55:58Z', 'before verification', 'ignored')`,
		`INSERT INTO facebook_message_receipts (external_message_id, page_id, psid, received_at, message_text, status) VALUES ('private-2', 'page-1', 'sender-2', '2026-09-30T13:56:58Z', 'must not leak', 'ignored')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := ListFacebookMessages(context.Background(), db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Text != "before verification" || messages[0].SenderID != "sender-1" || messages[0].Status != "ignored" {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestFacebookSchemaAndConfiguredPage(t *testing.T) {
	db := newFacebookStoreTest(t)
	for _, table := range []string{"facebook_integrations", "facebook_message_receipts"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO social_identities (user_id, platform, username, normalized_username, status) VALUES (1, 'threads', '', '', 'pending')`); err == nil {
		t.Fatal("Threads identity was accepted")
	}
	if _, err := db.Exec(`INSERT INTO notes (user_id, title, content_markdown, source) VALUES (1, 'x', 'x', 'facebook')`); err != nil {
		t.Fatalf("Facebook note source rejected: %v", err)
	}
}

func TestProcessFacebookMessageBindingRoutingIdempotencyAndRemoval(t *testing.T) {
	db := newFacebookStoreTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	aliceID := pendingFacebookIdentity(t, db, 1, "alice-code", now.Add(10*time.Minute))
	bobID := pendingFacebookIdentity(t, db, 2, "bob-code", now.Add(10*time.Minute))

	outcome, err := ProcessFacebookMessage(context.Background(), db, "page-1", "psid-alice", "bind-a", "alice-code", false, false, now.Format(time.RFC3339Nano))
	if err != nil || outcome.Kind != FacebookMessageActivated || outcome.IdentityID != aliceID {
		t.Fatalf("Alice binding = %#v, %v", outcome, err)
	}
	if outcome, err = ProcessFacebookMessage(context.Background(), db, "page-1", "psid-bob", "bind-b", "bob-code", false, false, now.Format(time.RFC3339Nano)); err != nil || outcome.Kind != FacebookMessageActivated || outcome.IdentityID != bobID {
		t.Fatalf("Bob binding = %#v, %v", outcome, err)
	}
	var notes int
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil || notes != 0 {
		t.Fatalf("binding created notes=%d err=%v", notes, err)
	}

	for _, message := range []struct {
		page, psid, id, text string
		echo, attachment     bool
	}{
		{"page-1", "psid-alice", "note-a", "Alice note", false, false},
		{"page-1", "psid-alice", "note-a", "duplicate", false, false},
		{"page-1", "psid-bob", "note-b", "Bob note", false, true},
		{"wrong-page", "psid-alice", "wrong-page", "ignored", false, false},
		{"page-1", "psid-alice", "echo", "ignored", true, false},
		{"page-1", "psid-alice", "empty", "   ", false, false},
		{"page-1", "psid-alice", "attachment", "", false, true},
		{"page-1", "unknown", "unknown", "ignored", false, false},
		{"page-1", "", "missing-psid", "ignored", false, false},
		{"page-1", "psid-alice", "", "ignored", false, false},
		{"page-1", "psid-alice", "   ", "ignored", false, false},
	} {
		if _, err := ProcessFacebookMessage(context.Background(), db, message.page, message.psid, message.id, message.text, message.echo, message.attachment, now.Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("process %#v: %v", message, err)
		}
	}
	var aliceNotes, bobNotes int
	if err := db.QueryRow(`SELECT count(*) FROM notes WHERE user_id=1 AND source='facebook' AND title='Facebook Messenger' AND content_markdown='Alice note'`).Scan(&aliceNotes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM notes WHERE user_id=2 AND source='facebook' AND title='Facebook Messenger' AND content_markdown='Bob note'`).Scan(&bobNotes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil || notes != 2 || aliceNotes != 1 || bobNotes != 1 {
		t.Fatalf("notes total=%d alice=%d bob=%d err=%v", notes, aliceNotes, bobNotes, err)
	}
	filtered, err := ListNotes(context.Background(), db, 1, "", "all", "facebook", "updated_at", "desc", 1, 20)
	if err != nil || filtered.Total != 1 || len(filtered.Notes) != 1 || filtered.Notes[0].Source != "facebook" {
		t.Fatalf("Facebook note filter=%#v err=%v", filtered, err)
	}

	if err := DeleteSocialIdentity(context.Background(), db, 1, aliceID); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessFacebookMessage(context.Background(), db, "page-1", "psid-alice", "after-remove", "ignored", false, false, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var preserved int
	if err := db.QueryRow(`SELECT count(*) FROM notes WHERE user_id=1 AND source='facebook' AND social_identity_id IS NULL`).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("preserved notes=%d err=%v", preserved, err)
	}
}

func TestProcessFacebookMessageRejectsExpiredConsumedAndWrongCodes(t *testing.T) {
	db := newFacebookStoreTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiredID := pendingFacebookIdentity(t, db, 1, "expired-code", now.Add(-time.Minute))

	for _, message := range []struct{ psid, id, text string }{
		{"expired-psid", "expired", "expired-code"},
		{"wrong-psid", "wrong", "wrong-code"},
	} {
		if _, err := ProcessFacebookMessage(context.Background(), db, "page-1", message.psid, message.id, message.text, false, false, now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	var status, platformUserID, verificationResult sql.NullString
	if err := db.QueryRow(`SELECT status, platform_user_id, verification_result FROM social_identities WHERE id=?`, expiredID).Scan(&status, &platformUserID, &verificationResult); err != nil {
		t.Fatal(err)
	}
	if status.String != "pending" || platformUserID.Valid || verificationResult.String != "expired" {
		t.Fatalf("expired identity status=%v psid=%v result=%v", status, platformUserID, verificationResult)
	}

	activeID := pendingFacebookIdentity(t, db, 2, "single-use", now.Add(time.Minute))
	if _, err := ProcessFacebookMessage(context.Background(), db, "page-1", "owner-psid", "activate", "single-use", false, false, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessFacebookMessage(context.Background(), db, "page-1", "other-psid", "reuse", "single-use", false, false, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var psid string
	if err := db.QueryRow(`SELECT platform_user_id FROM social_identities WHERE id=?`, activeID).Scan(&psid); err != nil || psid != "owner-psid" {
		t.Fatalf("active PSID=%q err=%v", psid, err)
	}
	var notes int
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&notes); err != nil || notes != 0 {
		t.Fatalf("invalid codes created notes=%d err=%v", notes, err)
	}
}

func TestProcessFacebookMessageRollsBackReceiptOnFailure(t *testing.T) {
	db := newFacebookStoreTest(t)
	now := time.Now().UTC()
	pendingFacebookIdentity(t, db, 1, "code", now.Add(time.Minute))
	if _, err := db.Exec(`CREATE TRIGGER fail_facebook_activation BEFORE UPDATE OF status ON social_identities WHEN NEW.platform='facebook' BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := ProcessFacebookMessage(context.Background(), db, "page-1", "psid", "retryable", "code", false, false, now.Format(time.RFC3339Nano))
	if err == nil {
		t.Fatal("forced processing failure succeeded")
	}
	var receipts int
	if err := db.QueryRow(`SELECT count(*) FROM facebook_message_receipts WHERE external_message_id='retryable'`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("receipt claim survived rollback: count=%d err=%v", receipts, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_facebook_activation`); err != nil {
		t.Fatal(err)
	}
	if outcome, err := ProcessFacebookMessage(context.Background(), db, "page-1", "psid", "retryable", "code", false, false, now.Format(time.RFC3339Nano)); err != nil || outcome.Kind != FacebookMessageActivated {
		t.Fatalf("retry = %#v, %v", outcome, err)
	}
}

func TestFacebookPageChangeInvalidatesPageScopedBindingsAndPreservesNotes(t *testing.T) {
	db := newFacebookStoreTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	identityID := pendingFacebookIdentity(t, db, 1, "bind-code", now.Add(time.Minute))
	if _, err := ProcessFacebookMessage(context.Background(), db, "page-1", "shared-psid", "bind", "bind-code", false, false, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessFacebookMessage(context.Background(), db, "page-1", "shared-psid", "note", "preserved", false, false, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureFacebookIntegration(context.Background(), db, "page-2"); err != nil {
		t.Fatal(err)
	}
	var identities, preserved int
	if err := db.QueryRow(`SELECT count(*) FROM social_identities WHERE id=?`, identityID).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM notes WHERE content_markdown='preserved' AND social_identity_id IS NULL`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if identities != 0 || preserved != 1 {
		t.Fatalf("page change left identities=%d preserved notes=%d", identities, preserved)
	}
	outcome, err := ProcessFacebookMessage(context.Background(), db, "page-2", "shared-psid", "after-change", "must not route", false, false, now.Format(time.RFC3339Nano))
	if err != nil || outcome.Kind != FacebookMessageIgnored {
		t.Fatalf("old page binding routed after change: %#v %v", outcome, err)
	}
}

func TestFacebookPendingIdentityIsOnePerUserAndRegenerationIsPlatformSpecific(t *testing.T) {
	db := newFacebookStoreTest(t)
	now := time.Now().UTC()
	id := pendingFacebookIdentity(t, db, 1, "first", now.Add(time.Minute))
	if _, err := CreatePendingFacebookIdentity(context.Background(), db, 1, "another-user", []byte("hash"), now.Add(time.Minute).Format(time.RFC3339Nano)); !errors.Is(err, ErrPlatformIdentityAlreadyRegistered) {
		t.Fatalf("duplicate Facebook identity error=%v", err)
	}
	hash := sha256.Sum256([]byte("replacement"))
	if _, err := ReplaceFacebookIdentityVerification(context.Background(), db, 1, id, hash[:], now.Add(10*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	instagram, err := CreatePendingSocialIdentity(context.Background(), db, 1, "instagram", "", "alice", "", "", []byte("instagram-hash"), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReplaceFacebookIdentityVerification(context.Background(), db, 1, instagram.ID, hash[:], now.Add(time.Minute).Format(time.RFC3339Nano)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Facebook regeneration changed Instagram identity: %v", err)
	}
}
