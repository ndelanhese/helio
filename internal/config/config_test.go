package config

import "testing"

func TestLoadUsesContainerSafeDefaults(t *testing.T) {
	t.Setenv("HELIO_HTTP_ADDR", "")
	t.Setenv("HELIO_DATABASE_PATH", "")
	t.Setenv("HELIO_ALEXA_RELAY_URL", "")
	t.Setenv("HELIO_ALEXA_SHARED_SECRET", "")

	cfg := Load()
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DatabasePath != "/data/helio.db" {
		t.Fatalf("DatabasePath = %q, want /data/helio.db", cfg.DatabasePath)
	}
	if cfg.AlexaRelayURL != "" || cfg.AlexaRelaySecret != "" {
		t.Fatalf("Alexa relay unexpectedly configured: %#v", cfg)
	}
}
