// Package dbtest provides an isolated Postgres schema for integration tests.
package dbtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool returns a pool whose search_path is a fresh schema (dropped after the
// test), or skips the test when PHALBUM_TEST_DATABASE_URL isn't set. The
// pgvector extension is installed in public, which stays on the path, so
// packages tested in parallel don't fight over it.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PHALBUM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHALBUM_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Two packages creating the extension at once can collide; retry once.
	for i := 0; i < 3; i++ {
		if _, err = admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector SCHEMA public"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close(context.Background())
	})

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	pool, err := pgxpool.New(ctx, dsn+sep+"search_path="+schema+",public")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
