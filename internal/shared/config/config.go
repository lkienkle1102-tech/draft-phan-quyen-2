// Package config loads application configuration from the process environment.
package config

import (
	"os"
	"time"
)

const defaultDatabasePath = "phan-quyen.db"
const defaultHTTPAddress = ":8080"

// Config contains the application runtime configuration.
type Config struct {
	DatabasePath string
	HTTPAddress  string
	Casdoor      Casdoor
}

type Casdoor struct {
	Endpoint, ClientID, ClientSecret, Certificate string
	Organization, Application                     string
	PermissionID, ModelID, ResourceID             string
	EnforcerID, Owner                             string
	HTTPTimeout                                   time.Duration
}

// Load reads application configuration from the process environment.
func Load() Config {
	databasePath := os.Getenv("SQLITE_PATH")
	if databasePath == "" {
		databasePath = defaultDatabasePath
	}

	return Config{
		DatabasePath: databasePath,
		HTTPAddress:  value("HTTP_ADDRESS", defaultHTTPAddress),
		Casdoor: Casdoor{
			Endpoint:     value("CASDOOR_ENDPOINT", "http://localhost:8000"),
			ClientID:     os.Getenv("CASDOOR_CLIENT_ID"),
			ClientSecret: os.Getenv("CASDOOR_CLIENT_SECRET"),
			Certificate:  os.Getenv("CASDOOR_CERTIFICATE"),
			Organization: value("CASDOOR_ORGANIZATION", "identity"),
			Application:  value("CASDOOR_APPLICATION", "authorization-api"),
			PermissionID: value("CASDOOR_PERMISSION_ID", "app-authorization"),
			ModelID:      value("CASDOOR_MODEL_ID", "application-domain-rbac"),
			ResourceID:   value("CASDOOR_RESOURCE_ID", "application-policy-adapter"),
			EnforcerID:   value("CASDOOR_ENFORCER_ID", "application-enforcer"),
			Owner:        value("CASDOOR_OWNER", "admin"),
			HTTPTimeout:  3 * time.Second,
		},
	}
}

func value(key, fallback string) string {
	if current := os.Getenv(key); current != "" {
		return current
	}
	return fallback
}
