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
	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))
	devMode, err := parseDevMode(os.Getenv("WORKOS_DEV_MODE"))
	if err != nil {
		return Config{}, err
	}
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

func parseDevMode(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, errors.New("WORKOS_DEV_MODE must be true or false")
	}
	return b, nil
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
