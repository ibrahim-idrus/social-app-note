package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFacebookWebhookVerifyToken(t *testing.T) {
	for key := range supported {
		value, exists := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}

	path := filepath.Join(t.TempDir(), ".env")
	contents := "HTTP_ADDR=127.0.0.1:8080\nDATABASE_PATH=sqlite.db\nSESSION_COOKIE_SECURE=false\nINSTAGRAM_ENABLED=false\nFACEBOOK_WEBHOOK_VERIFY_TOKEN=facebook-secret\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.FacebookWebhookVerifyToken != "facebook-secret" {
		t.Fatalf("FacebookWebhookVerifyToken was not loaded")
	}
}

func TestFacebookMessengerFeatureUsesOnlyConfiguredPage(t *testing.T) {
	for key := range supported {
		value, exists := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	path := filepath.Join(t.TempDir(), ".env")
	contents := "HTTP_ADDR=127.0.0.1:8080\nDATABASE_PATH=sqlite.db\nSESSION_COOKIE_SECURE=false\nINSTAGRAM_ENABLED=false\nMESSENGER_PAGE_ID=page-1\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FacebookMessengerEnabled || got.FacebookPageID != "page-1" {
		t.Fatalf("Facebook config=%#v", got)
	}
}
