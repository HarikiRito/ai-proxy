// Package config loads process configuration from the environment.
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	DatabaseURL string
	CORSOrigins []string
	Debug       bool
}

func Load() (Config, error) {
	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))
	cfg := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Debug:       debug,
	}
	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		cfg.CORSOrigins = strings.Split(origins, ",")
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
