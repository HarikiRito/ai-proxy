// Package db opens the shared ent client on a SQLite connection (pure Go, no cgo).
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

const pingTimeout = 5 * time.Second

// Open returns the ent client and its pool; closing the client closes the pool.
// databaseURL looks like file:/path/app.db?_pragma=foreign_keys(1).
func Open(ctx context.Context, databaseURL string) (*ent.Client, *sql.DB, error) {
	conn, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("database unreachable: %w", err)
	}
	return ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, conn))), conn, nil
}
