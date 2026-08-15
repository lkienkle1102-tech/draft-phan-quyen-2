// Package config loads application configuration from the process environment.
package config

import "os"

const defaultDatabasePath = "phan-quyen.db"
const defaultHTTPAddress = ":8080"

// Config contains the application runtime configuration.
type Config struct {
	DatabasePath string
	JWTIssuer    string
	JWTAudience  string
	JWTSecret    string
	HTTPAddress  string
}

// Load reads application configuration from the process environment.
func Load() Config {
	databasePath := os.Getenv("SQLITE_PATH")
	if databasePath == "" {
		databasePath = defaultDatabasePath
	}

	return Config{DatabasePath: databasePath, JWTIssuer: value("JWT_ISSUER", "phan-quyen"), JWTAudience: value("JWT_AUDIENCE", "api"), JWTSecret: value("JWT_SECRET", "development-secret-change-me"), HTTPAddress: value("HTTP_ADDRESS", defaultHTTPAddress)}
}

func value(key, fallback string) string {
	if current := os.Getenv(key); current != "" {
		return current
	}
	return fallback
}
