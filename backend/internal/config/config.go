package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr                                                                   string
	DatabasePath                                                               string
	SessionCookieSecure                                                        bool
	InstagramEnabled                                                           bool
	InstagramInboxOwnerEmail                                                   string
	InstagramPollIntervalSeconds                                               int
	InstagramAppID, InstagramAppSecret                                         string
	InstagramDedicatedAccountID, InstagramDedicatedUsername                    string
	InstagramWebhookVerifyToken, InstagramAccessToken                          string
	InstagramUser1AccountID, InstagramUser1Username, InstagramAccessUser1Token string
	InstagramUser2AccountID, InstagramUser2Username, InstagramAccessUser2Token string
}

const instagramPermissions = "instagram_business_basic,instagram_business_manage_messages"

var supported = map[string]bool{
	"HTTP_ADDR":             true,
	"DATABASE_PATH":         true,
	"SESSION_COOKIE_SECURE": true,
	"INSTAGRAM_ENABLED":     true,
	"INSTAGRAM_APP_ID":      true, "INSTAGRAM_APP_SECRET": true,
	"INSTAGRAM_DEDICATED_ACCOUNT_ID": true, "INSTAGRAM_DEDICATED_USERNAME": true,
	"INSTAGRAM_WEBHOOK_VERIFY_TOKEN": true, "INSTAGRAM_ACCESS_TOKEN": true,
	"INSTAGRAM_PERMISSIONS":      true,
	"INSTAGRAM_USER1_ACCOUNT_ID": true, "INSTAGRAM_USER1_USERNAME": true, "INSTAGRAM_ACCESS_USER1_TOKEN": true,
	"INSTAGRAM_USER2_ACCOUNT_ID": true, "INSTAGRAM_USER2_USERNAME": true, "INSTAGRAM_ACCESS_USER2_TOKEN": true,
	"INSTAGRAM_INBOX_OWNER_EMAIL": true, "INSTAGRAM_POLL_INTERVAL_SECONDS": true,
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open environment file: %w", err)
	}
	defer file.Close()

	values := make(map[string]string, len(supported))
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return Config{}, fmt.Errorf("invalid environment line %d", line)
		}
		if !supported[key] {
			return Config{}, fmt.Errorf("unknown setting %s", key)
		}
		if _, exists := values[key]; exists {
			return Config{}, fmt.Errorf("duplicate setting %s", key)
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("read environment file: %w", err)
	}

	for key := range supported {
		if value, ok := os.LookupEnv(key); ok {
			values[key] = value
		}
	}
	for _, key := range []string{"HTTP_ADDR", "DATABASE_PATH", "SESSION_COOKIE_SECURE", "INSTAGRAM_ENABLED"} {
		if values[key] == "" {
			return Config{}, fmt.Errorf("%s is required", key)
		}
	}

	host, portText, err := net.SplitHostPort(values["HTTP_ADDR"])
	if err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR must be host:port: %w", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return Config{}, fmt.Errorf("HTTP_ADDR must use a loopback host")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("HTTP_ADDR must use a valid port")
	}

	if values["INSTAGRAM_ENABLED"] == "true" {
		return Config{}, fmt.Errorf("INSTAGRAM_ENABLED must remain false until the feasibility gate passes")
	}
	if values["INSTAGRAM_ENABLED"] != "false" {
		return Config{}, fmt.Errorf("INSTAGRAM_ENABLED must be false")
	}
	if values["SESSION_COOKIE_SECURE"] != "true" && values["SESSION_COOKIE_SECURE"] != "false" {
		return Config{}, fmt.Errorf("SESSION_COOKIE_SECURE must be true or false")
	}
	secureCookies := values["SESSION_COOKIE_SECURE"] == "true"

	instagramKeys := []string{
		"INSTAGRAM_APP_ID", "INSTAGRAM_APP_SECRET", "INSTAGRAM_DEDICATED_ACCOUNT_ID", "INSTAGRAM_DEDICATED_USERNAME",
		"INSTAGRAM_WEBHOOK_VERIFY_TOKEN", "INSTAGRAM_ACCESS_TOKEN", "INSTAGRAM_INBOX_OWNER_EMAIL",
		"INSTAGRAM_USER1_ACCOUNT_ID", "INSTAGRAM_ACCESS_USER1_TOKEN", "INSTAGRAM_USER2_ACCOUNT_ID", "INSTAGRAM_ACCESS_USER2_TOKEN",
	}
	instagramConfigured := false
	for _, key := range instagramKeys {
		instagramConfigured = instagramConfigured || values[key] != ""
	}
	if instagramConfigured && values["INSTAGRAM_PERMISSIONS"] != instagramPermissions {
		return Config{}, fmt.Errorf("INSTAGRAM_PERMISSIONS must be %s", instagramPermissions)
	}
	if instagramConfigured {
		for _, key := range []string{"INSTAGRAM_APP_ID", "INSTAGRAM_APP_SECRET", "INSTAGRAM_DEDICATED_ACCOUNT_ID", "INSTAGRAM_DEDICATED_USERNAME", "INSTAGRAM_WEBHOOK_VERIFY_TOKEN", "INSTAGRAM_ACCESS_TOKEN", "INSTAGRAM_INBOX_OWNER_EMAIL"} {
			if values[key] == "" {
				return Config{}, fmt.Errorf("%s is required when Instagram is configured", key)
			}
		}
	}
	for _, pair := range [][2]string{{"INSTAGRAM_USER1_ACCOUNT_ID", "INSTAGRAM_ACCESS_USER1_TOKEN"}, {"INSTAGRAM_USER2_ACCOUNT_ID", "INSTAGRAM_ACCESS_USER2_TOKEN"}} {
		if (values[pair[0]] == "") != (values[pair[1]] == "") {
			return Config{}, fmt.Errorf("%s and %s must be configured together", pair[0], pair[1])
		}
	}

	pollInterval := 60
	if raw := values["INSTAGRAM_POLL_INTERVAL_SECONDS"]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 15 || parsed > 3600 {
			return Config{}, fmt.Errorf("INSTAGRAM_POLL_INTERVAL_SECONDS must be 15-3600")
		}
		pollInterval = parsed
	}

	return Config{
		HTTPAddr:            values["HTTP_ADDR"],
		DatabasePath:        values["DATABASE_PATH"],
		SessionCookieSecure: secureCookies,
		InstagramEnabled:    false,
		InstagramAppID:      values["INSTAGRAM_APP_ID"], InstagramAppSecret: values["INSTAGRAM_APP_SECRET"],
		InstagramDedicatedAccountID: values["INSTAGRAM_DEDICATED_ACCOUNT_ID"], InstagramDedicatedUsername: values["INSTAGRAM_DEDICATED_USERNAME"],
		InstagramWebhookVerifyToken: values["INSTAGRAM_WEBHOOK_VERIFY_TOKEN"], InstagramAccessToken: values["INSTAGRAM_ACCESS_TOKEN"],
		InstagramUser1AccountID: values["INSTAGRAM_USER1_ACCOUNT_ID"], InstagramUser1Username: values["INSTAGRAM_USER1_USERNAME"], InstagramAccessUser1Token: values["INSTAGRAM_ACCESS_USER1_TOKEN"],
		InstagramUser2AccountID: values["INSTAGRAM_USER2_ACCOUNT_ID"], InstagramUser2Username: values["INSTAGRAM_USER2_USERNAME"], InstagramAccessUser2Token: values["INSTAGRAM_ACCESS_USER2_TOKEN"],
		InstagramInboxOwnerEmail: values["INSTAGRAM_INBOX_OWNER_EMAIL"], InstagramPollIntervalSeconds: pollInterval,
	}, nil
}
