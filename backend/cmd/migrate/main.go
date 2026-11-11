// Command migrate authors versioned atlas migrations in ./migrations; apply them with the atlas CLI (make migrate).
//
//	migrate diff <name>   write a migration from the ent schema; DATABASE_URL must be an EMPTY scratch database
//	migrate sum           recompute atlas.sum after hand-editing a migration
//
// Run from backend/ (make migrate-diff does) so the relative migrations path resolves.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"

	atlas "ariga.io/atlas/sql/migrate"
	entschema "entgo.io/ent/dialect/sql/schema"
)

const migrationsDir = "migrations"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: migrate <diff <name>|sum>")
	}
	dir, err := atlas.NewLocalDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("open migrations dir (run from repo root): %w", err)
	}
	switch args[0] {
	case "sum":
		sum, err := dir.Checksum()
		if err != nil {
			return err
		}
		return atlas.WriteSumFile(dir, sum)
	case "diff":
		if len(args) < 2 {
			return fmt.Errorf("usage: migrate diff <name>")
		}
		return diff(dir, args[1])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func diff(dir *atlas.LocalDir, name string) error {
	ctx := context.Background()
	client, _, err := db.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.Schema.NamedDiff(ctx, name,
		entschema.WithDir(dir),
		entschema.WithMigrationMode(entschema.ModeReplay),
		entschema.WithFormatter(atlas.DefaultFormatter))
}
