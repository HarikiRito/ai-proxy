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
