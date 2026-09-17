package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	for key := range supported {
		value, exists := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if exists {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		})
	}

	tests := []struct {
		name       string
		contents   string
		wantSecure bool
		wantErr    string
	}{
		{
			name: "valid",
			contents: "HTTP_ADDR=127.0.0.1:8080\n" +
				"DATABASE_PATH=sqlite.db\n" +
				"SESSION_COOKIE_SECURE=false\n" +
				"INSTAGRAM_ENABLED=false\n" +
				"INSTAGRAM_APP_ID=app\nINSTAGRAM_APP_SECRET=secret\nINSTAGRAM_DEDICATED_ACCOUNT_ID=17841426326903892\nINSTAGRAM_DEDICATED_USERNAME=akun_testing911\nINSTAGRAM_WEBHOOK_VERIFY_TOKEN=verify\nINSTAGRAM_ACCESS_TOKEN=token\n",
		},
		{
			name: "HTTPS cookies",
			contents: "HTTP_ADDR=127.0.0.1:8080\n" +
				"DATABASE_PATH=sqlite.db\n" +
				"SESSION_COOKIE_SECURE=true\n" +
				"INSTAGRAM_ENABLED=false\n",
			wantSecure: true,
		},
		{name: "missing value", contents: "HTTP_ADDR=127.0.0.1:8080\n", wantErr: "DATABASE_PATH is required"},
		{
			name:     "non-loopback address",
			contents: "HTTP_ADDR=0.0.0.0:8080\nDATABASE_PATH=sqlite.db\nSESSION_COOKIE_SECURE=false\nINSTAGRAM_ENABLED=false\n",
			wantErr:  "HTTP_ADDR must use a loopback host",
		},
		{
			name:     "instagram enabled before feasibility gate",
			contents: "HTTP_ADDR=127.0.0.1:8080\nDATABASE_PATH=sqlite.db\nSESSION_COOKIE_SECURE=false\nINSTAGRAM_ENABLED=true\n",
			wantErr:  "INSTAGRAM_ENABLED must remain false",
		},
		{
			name:     "non-literal disabled gate",
			contents: "HTTP_ADDR=127.0.0.1:8080\nDATABASE_PATH=sqlite.db\nSESSION_COOKIE_SECURE=false\nINSTAGRAM_ENABLED=0\n",
			wantErr:  "INSTAGRAM_ENABLED must be false",
		},
		{
			name:     "unknown setting",
			contents: "HTTP_ADDR=127.0.0.1:8080\nDATABASE_PATH=sqlite.db\nSESSION_COOKIE_SECURE=false\nINSTAGRAM_ENABLED=false\nEXTRA=value\n",
			wantErr:  "unknown setting EXTRA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(tt.contents), 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := Load(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.HTTPAddr != "127.0.0.1:8080" || got.DatabasePath != "sqlite.db" || got.SessionCookieSecure != tt.wantSecure || got.InstagramEnabled {
				t.Fatalf("Load() = %#v", got)
			}
			if tt.name == "valid" && (got.InstagramDedicatedAccountID != "17841426326903892" || got.InstagramDedicatedUsername != "akun_testing911" || got.InstagramAccessToken != "token") {
				t.Fatalf("Instagram config = %#v", got)
			}
		})
	}
}
