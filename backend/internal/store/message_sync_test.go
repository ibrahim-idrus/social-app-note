package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func messageSyncDB(t *testing.T) (*sql.DB, int64, int64) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'a','a@x.test','x'),(2,'b','b@x.test','x');
		INSERT INTO social_identities(id,user_id,platform,platform_user_id,username,normalized_username,status) VALUES(10,1,'instagram','sender-a','a','a','active'),(20,2,'facebook','sender-b','b','b','active')`); err != nil {
		t.Fatal(err)
	}
	return db, 10, 20
}

func TestMessageSyncQueueOldestFirstAndDedupe(t *testing.T) {
	db, account, _ := messageSyncDB(t)
	ctx := context.Background()
	for _, m := range []QueuedSocialMessage{
		{SocialIdentityID: account, Platform: "instagram", ExternalMessageID: "later", ProviderSentAt: "2026-01-02T00:00:00Z", SenderID: "sender-a", RecipientID: "inbox"},
		{SocialIdentityID: account, Platform: "instagram", ExternalMessageID: "b", ProviderSentAt: "2026-01-01T00:00:00Z", SenderID: "sender-a", RecipientID: "inbox"},
		{SocialIdentityID: account, Platform: "instagram", ExternalMessageID: "a", ProviderSentAt: "2026-01-01T00:00:00Z", SenderID: "sender-a", RecipientID: "inbox"},
	} {
		if added, err := EnqueueSocialMessage(ctx, db, m); err != nil || !added {
			t.Fatalf("enqueue=%v %v", added, err)
		}
	}
	if added, err := EnqueueSocialMessage(ctx, db, QueuedSocialMessage{SocialIdentityID: account, Platform: "instagram", ExternalMessageID: "a", ProviderSentAt: "2026-01-03T00:00:00Z", SenderID: "sender-a", RecipientID: "inbox"}); err != nil || added {
		t.Fatalf("duplicate=%v %v", added, err)
	}
	for _, want := range []string{"a", "b", "later"} {
		got, err := NextSocialMessage(ctx, db, account)
		if err != nil || got.ExternalMessageID != want {
			t.Fatalf("next=%#v err=%v want=%s", got, err, want)
		}
		if err := CompleteSocialMessage(ctx, db, got.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMessageSyncFailureStopsQueue(t *testing.T) {
	db, account, _ := messageSyncDB(t)
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		_, err := EnqueueSocialMessage(ctx, db, QueuedSocialMessage{SocialIdentityID: account, Platform: "instagram", ExternalMessageID: id, ProviderSentAt: "2026-01-01T00:00:00Z", SenderID: "sender-a", RecipientID: "inbox"})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, _ := NextSocialMessage(ctx, db, account)
	if err := FailSocialMessage(ctx, db, first.ID, "processor_failed"); err != nil {
		t.Fatal(err)
	}
	got, err := NextSocialMessage(ctx, db, account)
	if err != nil || got.ID != first.ID {
		t.Fatalf("failed row did not remain first: %#v %v", got, err)
	}
	processed, duplicates, err := DrainSocialMessages(ctx, db, account, func(QueuedSocialMessage) (bool, error) { return false, errors.New("still broken") })
	if err == nil || processed != 0 || duplicates != 0 {
		t.Fatalf("drain=%d,%d,%v", processed, duplicates, err)
	}
	var pending int
	if err := db.QueryRow(`SELECT count(*) FROM social_message_sync_queue WHERE social_identity_id=?`, account).Scan(&pending); err != nil || pending != 2 {
		t.Fatalf("pending=%d %v", pending, err)
	}
}

func TestMessageSyncLocksArePerAccountAndLeased(t *testing.T) {
	db, a, b := messageSyncDB(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if ok, err := AcquireSocialMessageLock(ctx, db, a, now, time.Minute); err != nil || !ok {
		t.Fatalf("first=%v %v", ok, err)
	}
	if ok, err := AcquireSocialMessageLock(ctx, db, a, now, time.Minute); err != nil || ok {
		t.Fatalf("same=%v %v", ok, err)
	}
	if ok, err := AcquireSocialMessageLock(ctx, db, b, now, time.Minute); err != nil || !ok {
		t.Fatalf("different=%v %v", ok, err)
	}
	if ok, err := AcquireSocialMessageLock(ctx, db, a, now.Add(2*time.Minute), time.Minute); err != nil || !ok {
		t.Fatalf("expired=%v %v", ok, err)
	}
}

func TestDrainSocialMessagesProcessesOldestAndStopsOnFailure(t *testing.T) {
	db, account, _ := messageSyncDB(t)
	ctx := context.Background()
	for _, m := range []QueuedSocialMessage{{SocialIdentityID: account, Platform: "instagram", ExternalMessageID: "new", ProviderSentAt: "2026-01-02T00:00:00Z", SenderID: "sender-a", RecipientID: "inbox"}, {SocialIdentityID: account, Platform: "instagram", ExternalMessageID: "old", ProviderSentAt: "2026-01-01T00:00:00Z", SenderID: "sender-a", RecipientID: "inbox"}} {
		if _, err := EnqueueSocialMessage(ctx, db, m); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	_, _, err := DrainSocialMessages(ctx, db, account, func(m QueuedSocialMessage) (bool, error) {
		seen = append(seen, m.ExternalMessageID)
		if m.ExternalMessageID == "old" {
			return false, errors.New("fail")
		}
		return false, nil
	})
	if err == nil || len(seen) != 1 || seen[0] != "old" {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
}
