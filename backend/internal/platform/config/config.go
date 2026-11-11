// Package config loads process configuration from the environment.
package config

import (
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	DatabaseURL string
	CORSOrigins []string
	Debug       bool

	WorkOSClientID string
	WorkOSAPIKey   string
	WorkOSIssuer   string
	OwnerEmail     string
	WorkOSDevMode  bool
}

func Load() (Config, error) {
	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))
	devMode, _ := strconv.ParseBool(os.Getenv("WORKOS_DEV_MODE"))
	cfg := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Debug:       debug,

		WorkOSClientID: trimmedEnv("WORKOS_CLIENT_ID"),
		WorkOSAPIKey:   trimmedEnv("WORKOS_API_KEY"),
		WorkOSIssuer:   trimmedEnv("WORKOS_ISSUER"),
		OwnerEmail:     trimmedEnv("OWNER_EMAIL"),
		WorkOSDevMode:  devMode,
	}
	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		cfg.CORSOrigins = strings.Split(origins, ",")
	}
	return cfg, requireEnv(map[string]string{
		"DATABASE_URL":     cfg.DatabaseURL,
		"WORKOS_CLIENT_ID": cfg.WorkOSClientID,
		"WORKOS_API_KEY":   cfg.WorkOSAPIKey,
		"WORKOS_ISSUER":    cfg.WorkOSIssuer,
		"OWNER_EMAIL":      cfg.OwnerEmail,
	})
}

func trimmedEnv(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireEnv(vars map[string]string) error {
	var missing []string
	for name, value := range vars {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	return errors.New("missing required env: " + strings.Join(missing, ", "))
}
