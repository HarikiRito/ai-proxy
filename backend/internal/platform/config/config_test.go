package config

import "testing"

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("want error for missing DATABASE_URL")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "file:x.db")
	t.Setenv("PORT", "")
	t.Setenv("CORS_ORIGINS", "http://a,http://b")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || len(cfg.CORSOrigins) != 2 {
		t.Fatalf("unexpected config %+v", cfg)
	}
}
