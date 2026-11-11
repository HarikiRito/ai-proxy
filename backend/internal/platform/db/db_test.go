package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

// DSN options the deployment uses: writers queue instead of failing with SQLITE_BUSY.
const dsnOptions = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"

func openPool(t *testing.T) *sql.DB {
	t.Helper()
	client, pool, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+dsnOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := pool.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, n INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(`INSERT INTO t (id, n) VALUES (1, 0)`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestWriteWaitsForOpenTransactionInsteadOfFailingBusy(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	tx1, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx1.Exec(`UPDATE t SET n = n + 1`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := pool.ExecContext(ctx, `UPDATE t SET n = n + 10`)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if err := tx1.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second writer failed instead of waiting: %v", err)
	}
	var n int
	if err := pool.QueryRow(`SELECT n FROM t`).Scan(&n); err != nil || n != 11 {
		t.Fatalf("n = %d, err = %v; want 11", n, err)
	}
}

func TestConcurrentReadThenWriteTransactionsAllSucceed(t *testing.T) {
	pool := openPool(t)
	const workers = 8
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- readThenWrite(context.Background(), pool)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("read-then-write tx failed: %v", err)
		}
	}
	var n int
	if err := pool.QueryRow(`SELECT n FROM t`).Scan(&n); err != nil || n != workers {
		t.Fatalf("n = %d, err = %v; want %d", n, err, workers)
	}
}

func readThenWrite(ctx context.Context, pool *sql.DB) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRow(`SELECT n FROM t`).Scan(&n); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := tx.Exec(`UPDATE t SET n = ?`, n+1); err != nil {
		return err
	}
	return tx.Commit()
}
