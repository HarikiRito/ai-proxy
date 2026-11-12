package backend_test

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGoModHasNoPostgresDeps(t *testing.T) {
	for _, file := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"pgx", "testcontainers", "jackc"} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s still mentions %s", file, banned)
			}
		}
	}
}

func migrate(t *testing.T, dbFile string) string {
	t.Helper()
	if _, err := exec.LookPath("atlas"); err != nil {
		t.Fatal("atlas CLI required on PATH (https://atlasgo.io)")
	}
	var out bytes.Buffer
	cmd := exec.Command("make", "--no-print-directory", "migrate", "DB_FILE="+dbFile)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("make migrate: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestMakeMigrateCreatesMissingDatabaseAndIsIdempotent(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "nested", "app.db")

	migrate(t, dbFile)

	conn, err := sql.Open("sqlite", "file:"+dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	tableExists := func(name string) bool {
		var found string
		err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
		return err == nil
	}
	for _, table := range []string{"providers_providers", "providers_keys", "proxykeys_keys", "virtualmodels_models", "virtualmodels_tiers", "virtualmodels_entries"} {
		if !tableExists(table) {
			t.Errorf("%s table missing after migrate", table)
		}
	}
	if tableExists("settings") {
		t.Error("placeholder settings table must be dropped")
	}

	if out := migrate(t, dbFile); !strings.Contains(out, "No migration files to execute") {
		t.Fatalf("second apply should be a no-op, got:\n%s", out)
	}
}

func TestClaudeAccountsMigrationKeepsExistingKeysAsAPIKey(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "app.db")
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("atlas", append([]string{"migrate", "apply", "--url", "sqlite://" + dbFile + "?_fk=1", "--dir", "file://migrations"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("atlas: %v\n%s", err, out)
		}
	}
	run("--to-version", "20261112095615")
	conn, err := sql.Open("sqlite", "file:"+dbFile+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	for _, q := range []string{
		`INSERT INTO providers_providers (id, name, kind, base_url, enabled, cache_affinity_seconds, created_at, updated_at) VALUES ('p1', 'openai', 'openai_compatible', 'https://x.test', 1, 600, '2026-01-01', '2026-01-01')`,
		`INSERT INTO providers_keys (id, name, ciphertext, enabled, max_in_flight, weight, created_at, updated_at, provider_keys) VALUES ('k1', 'main', x'01', 1, 2, 1, '2026-01-01', '2026-01-01', 'p1')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	run()
	var mode string
	var created sql.NullString
	if err := conn.QueryRow(`SELECT auth_mode, token_created_at FROM providers_keys WHERE id = 'k1'`).Scan(&mode, &created); err != nil {
		t.Fatal(err)
	}
	if mode != "api_key" || created.Valid {
		t.Fatalf("existing key became %q %v", mode, created)
	}
}
