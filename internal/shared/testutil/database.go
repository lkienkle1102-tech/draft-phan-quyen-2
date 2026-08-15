// Package testutil provides local SQLite test setup.
package testutil

import (
	"database/sql"
	"testing"

	"example.com/phan-quyen-golang/internal/shared/app"
	"example.com/phan-quyen-golang/internal/shared/database/sqlite"
)

func Database(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sqlite.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := app.Migrate(database); err != nil {
		t.Fatal(err)
	}
	return database
}
