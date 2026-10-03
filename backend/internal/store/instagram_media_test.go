package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestInstagramMediaClaimCooldownStaleAndCascade(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'A','a@b.c','x'); INSERT INTO notes(id,user_id,title,content_markdown,source) VALUES(1,1,'n','','instagram')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	claimed := 0
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _, e := ClaimInstagramMedia(ctx, db, 1, "0", now)
			if e != nil {
				t.Error(e)
			}
			if c {
				claimed++
			}
		}()
	}
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("claims=%d", claimed)
	}
	if err := FailInstagramMedia(ctx, db, 1, "0", now); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := ClaimInstagramMedia(ctx, db, 1, "0", now.Add(4*time.Minute)); c {
		t.Fatal("claimed during cooldown")
	}
	if c, _, _ := ClaimInstagramMedia(ctx, db, 1, "0", now.Add(5*time.Minute)); !c {
		t.Fatal("did not claim after cooldown")
	}
	if c, _, _ := ClaimInstagramMedia(ctx, db, 1, "0", now.Add(5*time.Minute+59*time.Second)); c {
		t.Fatal("claimed fresh in-flight row")
	}
	if c, _, _ := ClaimInstagramMedia(ctx, db, 1, "0", now.Add(6*time.Minute)); !c {
		t.Fatal("did not reclaim stale claim")
	}
	if _, err := db.Exec(`DELETE FROM notes WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM instagram_media_cache`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade count=%d err=%v", count, err)
	}
}

func TestInstagramMediaAvailableIsPersistentHit(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, _ = db.Exec(`INSERT INTO users(id,name,email,password_hash) VALUES(1,'A','a@b.c','x'); INSERT INTO notes(id,user_id,title,content_markdown,source) VALUES(1,1,'n','','instagram')`)
	now := time.Now().UTC()
	if c, _, _ := ClaimInstagramMedia(context.Background(), db, 1, "0", now); !c {
		t.Fatal("missing claim")
	}
	items := []InstagramCachedMedia{{CacheKey: "abc", ContentType: "image/jpeg", Kind: "image", Position: 0}}
	if err := CompleteInstagramMedia(context.Background(), db, 1, "0", items, now); err != nil {
		t.Fatal(err)
	}
	if c, got, err := ClaimInstagramMedia(context.Background(), db, 1, "0", now.Add(time.Hour)); err != nil || c || len(got) != 1 || got[0].CacheKey != "abc" {
		t.Fatalf("claim=%v got=%#v err=%v", c, got, err)
	}
}
