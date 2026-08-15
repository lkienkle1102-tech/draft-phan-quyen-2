package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenConfiguresDatabase(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "test.sqlite")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	assertPragma(t, database, "foreign_keys", "1")
	assertPragma(t, database, "busy_timeout", "5000")
	assertPragma(t, database, "journal_mode", "wal")
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	database, err := Open("")
	if err == nil {
		if closeErr := database.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
		t.Fatal("Open() error = nil, want error")
	}
}

func assertPragma(t *testing.T, database *sql.DB, name, expected string) {
	t.Helper()

	var actual string
	if err := database.QueryRow("PRAGMA " + name).Scan(&actual); err != nil {
		t.Fatalf("query PRAGMA %s: %v", name, err)
	}
	if actual != expected {
		t.Fatalf("PRAGMA %s = %q, want %q", name, actual, expected)
	}
}
