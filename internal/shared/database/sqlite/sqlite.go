// Package sqlite creates configured SQLite database connections.
package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

const maxOpenConnections = 1

var pragmas = []string{
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 5000",
	"PRAGMA journal_mode = WAL",
}

// Open creates and verifies a SQLite database connection.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	database.SetMaxOpenConns(maxOpenConnections)

	if err := configure(database); err != nil {
		closeErr := database.Close()
		return nil, errors.Join(err, closeErr)
	}

	return database, nil
}

func configure(database *sql.DB) error {
	for _, pragma := range pragmas {
		if _, err := database.Exec(pragma); err != nil {
			return fmt.Errorf("execute %q: %w", pragma, err)
		}
	}

	if err := database.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return nil
}
