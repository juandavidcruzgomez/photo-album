package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/dbtest"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := New(context.Background(), dbtest.Pool(t))
	if err != nil {
		t.Fatal(err)
	}
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

func TestListenerWait(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	l, err := s.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	start := time.Now()
	if err := l.Wait(ctx, 200*time.Millisecond); err != nil {
		t.Fatalf("timeout should return nil, got %v", err)
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Error("Wait returned before the timeout without a notification")
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Enqueue(ctx, newJob("h1", "/a/x.jpg"))
	}()
	start = time.Now()
	if err := l.Wait(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("notification took %v", d)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := l.Wait(cctx, time.Second); err == nil {
		t.Error("Wait on a cancelled context should fail")
	}
}

func TestRequeueStale(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.Enqueue(ctx, newJob("old", "/a/old.jpg"))
	s.Enqueue(ctx, newJob("new", "/a/new.jpg"))
	old, _ := s.Claim(ctx)
	s.Claim(ctx)
	s.pool.Exec(ctx, "UPDATE index_jobs SET updated_at = now() - interval '1 hour' WHERE job_id = $1", old.ID)

	n, err := s.RequeueStale(ctx, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("requeued %d, err=%v", n, err)
	}
	j, err := s.Claim(ctx)
	if err != nil || j.Hash != "old" || j.Attempts != 2 {
		t.Errorf("reclaimed %+v, err=%v", j, err)
	}
}
