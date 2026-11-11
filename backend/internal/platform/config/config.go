// Package config loads process configuration from the environment.
package config

import (
	"encoding/base64"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
)

const masterKeyBytes = 32

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

	// MasterKey seals provider API keys at rest: 32 raw bytes from base64 MASTER_KEY.
	MasterKey []byte
}

func Load() (Config, error) {
	var err error
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
	masterKey := trimmedEnv("MASTER_KEY")
	if err := requireEnv(map[string]string{
		"DATABASE_URL":     cfg.DatabaseURL,
		"WORKOS_CLIENT_ID": cfg.WorkOSClientID,
		"WORKOS_API_KEY":   cfg.WorkOSAPIKey,
		"WORKOS_ISSUER":    cfg.WorkOSIssuer,
		"OWNER_EMAIL":      cfg.OwnerEmail,
		"MASTER_KEY":       masterKey,
	}); err != nil {
		return cfg, err
	}
	cfg.MasterKey, err = decodeMasterKey(masterKey)
	return cfg, err
}

func decodeMasterKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != masterKeyBytes {
		return nil, errors.New("MASTER_KEY must be base64 of exactly 32 bytes (generate with: openssl rand -base64 32)")
	}
	return key, nil
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
