package main

import (
	"os"
	"strings"
	"testing"
)

func TestServerUsesWebhookOnlyForInstagramInbound(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "StartInstagramPoller") {
		t.Fatal("server startup must not launch the Instagram poller")
	}
	for _, required := range []string{"ConfigureInstagramIntegration", "ConfigureInstagramIntegrationOwner", "log.Printf(\"instagram inbox owner unavailable: %v\", err)"} {
		if !strings.Contains(text, required) {
			t.Fatalf("server startup must configure Instagram and degrade safely when its owner is unavailable: missing %s", required)
		}
	}
	if strings.Contains(text, "ConfigureInstagramIntegrationOwner(context.Background(), db, cfg.InstagramDedicatedAccountID, cfg.InstagramInboxOwnerEmail); err != nil {\n		log.Fatal(err)") {
		t.Fatal("a missing Instagram inbox owner must not stop the unrelated API")
	}
}
