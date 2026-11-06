// Package health checks that a component can reach what it depends on: the
// database, object storage, the photos folder and its model files. The checks
// only read; they never create anything.
package health

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

// timeout bounds each check, so an unreachable host can't hang the report.
const timeout = 10 * time.Second

// Check is one dependency to verify. Run returns a short description on
// success.
type Check struct {
	Name string
	Run  func(ctx context.Context) (string, error)
}

// Run executes the checks in order, prints one line per check to w, and
// returns an error if any of them failed.
func Run(ctx context.Context, w io.Writer, checks ...Check) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	failed := 0
	for _, c := range checks {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		detail, err := c.Run(cctx)
		elapsed := time.Since(start).Round(time.Millisecond)
		cancel()
		status := "ok"
		if err != nil {
			// Connection errors can span lines, which would break the table.
			status, detail = "FAIL", strings.Join(strings.Fields(err.Error()), " ")
			failed++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", status, c.Name, elapsed, detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(checks))
	}
	return nil
}

// Database connects to Postgres and verifies that pgvector is available.
func Database(dsn string) Check {
	return Check{"database", func(ctx context.Context) (string, error) {
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return "", err
		}
		defer conn.Close(ctx)
		var version string
		err = conn.QueryRow(ctx,
			"SELECT default_version FROM pg_available_extensions WHERE name = 'vector'").Scan(&version)
		if err != nil {
			return "", fmt.Errorf("pgvector not available: %w", err)
		}
		cfg := conn.Config()
		return fmt.Sprintf("%s:%d, pgvector %s", cfg.Host, cfg.Port, version), nil
	}}
}

// Storage verifies that the S3 endpoint is reachable and accepts the
// credentials. A missing bucket is fine: the api and indexer create it.
func Storage(cfg storage.S3Config) Check {
	return Check{"storage", func(ctx context.Context) (string, error) {
		exists, err := storage.Ping(ctx, cfg)
		if err != nil {
			return "", err
		}
		if !exists {
			return fmt.Sprintf("%s, bucket %s not created yet", cfg.Endpoint, cfg.Bucket), nil
		}
		return fmt.Sprintf("%s, bucket %s", cfg.Endpoint, cfg.Bucket), nil
	}}
}

// Photos verifies that root can be listed and has something in it: a network
// share that isn't mounted usually looks like an empty directory. An empty
// root means the location isn't configured, which fails too.
func Photos(root string) Check {
	return Check{"photos", func(context.Context) (string, error) {
		if root == "" {
			return "", fmt.Errorf("PHOTOS_ROOT is not set")
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "", fmt.Errorf("%s is empty; is the share mounted?", root)
		}
		return fmt.Sprintf("%s, %d entries", root, len(entries)), nil
	}}
}

// Files verifies that every path is a non-empty file.
func Files(name string, paths ...string) Check {
	return Check{name, func(context.Context) (string, error) {
		var bad []string
		for _, p := range paths {
			if info, err := os.Stat(p); err != nil || info.IsDir() || info.Size() == 0 {
				bad = append(bad, p)
			}
		}
		if len(bad) > 0 {
			return "", fmt.Errorf("missing or empty: %s", strings.Join(bad, ", "))
		}
		return fmt.Sprintf("%d files", len(paths)), nil
	}}
}
