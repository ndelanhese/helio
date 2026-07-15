package config

import "testing"

func TestLoadUsesContainerSafeDefaults(t *testing.T) {
	t.Setenv("HELIO_HTTP_ADDR", "")
	t.Setenv("HELIO_DATABASE_PATH", "")

	cfg := Load()
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DatabasePath != "/data/helio.db" {
		t.Fatalf("DatabasePath = %q, want /data/helio.db", cfg.DatabasePath)
	}
}
