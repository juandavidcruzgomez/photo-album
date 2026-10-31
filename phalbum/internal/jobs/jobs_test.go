package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// openTest returns a Store on a fresh schema, or skips when no test database
// is configured via PHALBUM_TEST_DATABASE_URL.
func openTest(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("PHALBUM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHALBUM_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("test_%d", time.Now().UnixNano())

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		admin.Close(context.Background())
	})

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	s, err := Open(ctx, dsn+sep+"search_path="+schemaName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func newJob(hash, path string) NewJob {
	return NewJob{
		Hash: hash, Path: path, Size: 42, ModTime: time.Now(),
		MimeType: "image/jpeg", Extension: "jpg",
		Tags: []string{"2024", "Paris"}, Exif: map[string]string{"Model": "X100"},
	}
}

func TestEnqueueDedup(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	created, err := s.Enqueue(ctx, newJob("h1", "/a/x.jpg"))
	if err != nil || !created {
		t.Fatalf("first enqueue: created=%v err=%v", created, err)
	}
	created, err = s.Enqueue(ctx, newJob("h1", "/a/x.jpg"))
	if err != nil || created {
		t.Fatalf("same file again: created=%v err=%v", created, err)
	}
	created, err = s.Enqueue(ctx, newJob("h1", "/b/copy.jpg"))
	if err != nil || created {
		t.Fatalf("copy elsewhere: created=%v err=%v", created, err)
	}

	known, err := s.KnownFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(known) != 2 || known["/b/copy.jpg"].Hash != "h1" {
		t.Errorf("known files = %+v", known)
	}
	j := newJob("h1", "/a/x.jpg")
	if !known["/a/x.jpg"].Unchanged(j.Size, known["/a/x.jpg"].ModTime) {
		t.Error("Unchanged should be true for recorded size and mtime")
	}

	var jobs int
	s.pool.QueryRow(ctx, "SELECT count(*) FROM index_jobs").Scan(&jobs)
	if jobs != 1 {
		t.Errorf("got %d jobs, want 1", jobs)
	}
}

func TestClaimLifecycle(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	if _, err := s.Claim(ctx); !errors.Is(err, ErrNoJobs) {
		t.Fatalf("empty queue: err=%v", err)
	}
	s.Enqueue(ctx, newJob("h1", "/a/x.jpg"))
	s.Enqueue(ctx, newJob("h2", "/a/y.jpg"))

	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if j.Hash != "h1" || j.Attempts != 1 || j.URI != "file:///a/x.jpg" || j.Filename != "x.jpg" ||
		j.Exif["Model"] != "X100" || len(j.Tags) != 2 {
		t.Errorf("claimed %+v", j)
	}
	if err := s.Complete(ctx, j.ID); err != nil {
		t.Fatal(err)
	}

	j2, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(ctx, j2.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx); !errors.Is(err, ErrNoJobs) {
		t.Fatalf("drained queue: err=%v", err)
	}

	var status, msg string
	s.pool.QueryRow(ctx, "SELECT status, message FROM index_jobs WHERE job_id = $1", j2.ID).Scan(&status, &msg)
	if status != StatusError || msg != "boom" {
		t.Errorf("failed job: status=%s msg=%s", status, msg)
	}
}

func TestConcurrentClaim(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.Enqueue(ctx, newJob("h1", "/a/x.jpg"))

	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Claim(ctx)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	won := 0
	for err := range results {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrNoJobs):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d claims succeeded, want 1", won)
	}
}
