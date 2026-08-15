package config

import "testing"

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
