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
	if strings.Contains(string(source), "StartInstagramPoller") {
		t.Fatal("server startup must not launch the Instagram poller")
	}
	for _, required := range []string{"ConfigureInstagramIntegration", "ConfigureInstagramIntegrationOwner", "log.Fatal"} {
		if !strings.Contains(string(source), required) {
			t.Fatalf("server startup must fail while configuring Instagram: missing %s", required)
		}
	}
}
