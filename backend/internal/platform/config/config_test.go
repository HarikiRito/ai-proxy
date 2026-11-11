package config

import "testing"

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "file:x.db")
	t.Setenv("WORKOS_CLIENT_ID", "client_x")
	t.Setenv("WORKOS_API_KEY", "sk_x")
	t.Setenv("WORKOS_ISSUER", "https://issuer.test/")
	t.Setenv("OWNER_EMAIL", "owner@example.com")
}

func TestLoadRequiresEnv(t *testing.T) {
	for _, name := range []string{"DATABASE_URL", "WORKOS_CLIENT_ID", "WORKOS_API_KEY", "WORKOS_ISSUER", "OWNER_EMAIL"} {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(name, "")
			if _, err := Load(); err == nil {
				t.Fatalf("want error for missing %s", name)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "")
	t.Setenv("CORS_ORIGINS", "http://a,http://b")
	t.Setenv("WORKOS_DEV_MODE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || len(cfg.CORSOrigins) != 2 || cfg.WorkOSDevMode {
		t.Fatalf("unexpected config %+v", cfg)
	}
	if cfg.WorkOSClientID != "client_x" || cfg.WorkOSAPIKey != "sk_x" ||
		cfg.WorkOSIssuer != "https://issuer.test/" || cfg.OwnerEmail != "owner@example.com" {
		t.Fatalf("workos config not loaded: %+v", cfg)
	}
}

func TestLoadDevMode(t *testing.T) {
	setRequired(t)
	t.Setenv("WORKOS_DEV_MODE", "true")

	cfg, err := Load()
	if err != nil || !cfg.WorkOSDevMode {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}

func TestLoadTrimsRequiredValues(t *testing.T) {
	setRequired(t)
	t.Setenv("OWNER_EMAIL", " owner@x.com\n")

	cfg, err := Load()
	if err != nil || cfg.OwnerEmail != "owner@x.com" {
		t.Fatalf("got %q, %v", cfg.OwnerEmail, err)
	}
}

func TestLoadTreatsWhitespaceOnlyAsMissing(t *testing.T) {
	setRequired(t)
	t.Setenv("WORKOS_API_KEY", " \t\n")

	if _, err := Load(); err == nil {
		t.Fatal("want error for whitespace-only WORKOS_API_KEY")
	}
}
