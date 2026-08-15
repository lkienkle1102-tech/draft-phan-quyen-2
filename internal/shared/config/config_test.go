package config

import (
	"testing"
	"time"
)

func TestLoadUsesDefaultDatabasePath(t *testing.T) {
	t.Setenv("SQLITE_PATH", "")

	cfg := Load()
	if cfg.DatabasePath != defaultDatabasePath {
		t.Fatalf("Load().DatabasePath = %q, want %q", cfg.DatabasePath, defaultDatabasePath)
	}
}

func TestLoadUsesConfiguredDatabasePath(t *testing.T) {
	const databasePath = "data/test.sqlite"
	t.Setenv("SQLITE_PATH", databasePath)

	cfg := Load()
	if cfg.DatabasePath != databasePath {
		t.Fatalf("Load().DatabasePath = %q, want %q", cfg.DatabasePath, databasePath)
	}
}

func TestLoadReadsCasdoorConfiguration(t *testing.T) {
	t.Setenv("CASDOOR_CLIENT_ID", "client")
	t.Setenv("CASDOOR_CLIENT_SECRET", "secret")
	t.Setenv("CASDOOR_CERTIFICATE", "certificate")

	cfg := Load()
	if cfg.Casdoor.ClientID != "client" || cfg.Casdoor.ClientSecret != "secret" || cfg.Casdoor.Certificate != "certificate" {
		t.Fatalf("Load().Casdoor = %+v", cfg.Casdoor)
	}
	if cfg.Casdoor.HTTPTimeout != 3*time.Second {
		t.Fatalf("Load().Casdoor.HTTPTimeout = %s", cfg.Casdoor.HTTPTimeout)
	}
}
