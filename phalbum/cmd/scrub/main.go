// Command scrub reads every thumbnail and stored original back from object
// storage, which makes SeaweedFS verify their checksums, and rebuilds the
// missing or corrupted ones from the source files.
//
//	go run ./cmd/scrub [-dry-run] [-only thumbnails|originals] [-concurrency 1]
//
// Rebuilding decodes and hashes photos, so on a small machine (a Pi) run it
// from another machine that sees the same database, storage and photo paths.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/repair"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/settings"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type counts struct {
	photos, checked, ok, broken, repaired, unrecoverable atomic.Int64
}

func run() error {
	cfg, err := settings.Load()
	if err != nil {
		return err
	}
	dsn := flag.String("db", "", "Postgres connection string (default: from settings)")
	dryRun := flag.Bool("dry-run", false, "only report broken objects, don't rebuild them")
	only := flag.String("only", "", `check only "thumbnails" or "originals" (default: both)`)
	concurrency := flag.Int("concurrency", 1, "photos checked at once")
	timeout := flag.Duration("timeout", time.Minute, "how long to wait for one object before calling it broken")
	flag.Parse()
	if *only != "" && *only != "thumbnails" && *only != "originals" {
		return fmt.Errorf(`-only: use "thumbnails" or "originals"`)
	}
	if *dsn == "" {
		*dsn = cfg.DSN()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	lib, err := library.New(ctx, pool)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	objects, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey,
		// A corrupted object fails the same way every time; SeaweedFS already
		// retries internally, so one attempt is enough.
		MaxAttempts: 1,
	})
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	s := &scrubber{objects: objects, dryRun: *dryRun, timeout: *timeout,
		thumbnails: *only != "originals", originals: *only != "thumbnails"}
	start := time.Now()
	progress := time.NewTicker(30 * time.Second)
	defer progress.Stop()
	go func() {
		for range progress.C {
			fmt.Fprintf(os.Stderr, "… %d photos, %d broken so far\n", s.n.photos.Load(), s.n.broken.Load())
		}
	}()

	work := make(chan *library.Files)
	var wg sync.WaitGroup
	for range max(*concurrency, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range work {
				s.check(ctx, f)
			}
		}()
	}
	err = lib.EachFiles(ctx, func(f *library.Files) error {
		select {
		case work <- f:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(work)
	wg.Wait()

	n := &s.n
	fmt.Printf("\n%d photos, %d objects checked in %s: %d ok, %d broken",
		n.photos.Load(), n.checked.Load(), time.Since(start).Round(time.Second), n.ok.Load(), n.broken.Load())
	if !*dryRun {
		fmt.Printf(" (%d repaired, %d unrecoverable)", n.repaired.Load(), n.unrecoverable.Load())
	}
	fmt.Println()
	if errors.Is(err, context.Canceled) {
		return errors.New("interrupted")
	}
	return err
}

type scrubber struct {
	objects               *storage.S3
	dryRun                bool
	timeout               time.Duration
	thumbnails, originals bool
	n                     counts
}

func (s *scrubber) check(ctx context.Context, f *library.Files) {
	s.n.photos.Add(1)
	if s.thumbnails {
		s.object(ctx, f, f.ThumbnailKey, func() error {
			_, err := repair.RebuildThumbnail(ctx, s.objects, f)
			return err
		})
	}
	if s.originals && f.OriginalKey != "" {
		s.object(ctx, f, f.OriginalKey, func() error {
			return repair.RebuildOriginal(ctx, s.objects, f)
		})
	}
}

// object reads one object fully, and rebuilds it when that fails.
func (s *scrubber) object(ctx context.Context, f *library.Files, key string, rebuild func() error) {
	if ctx.Err() != nil {
		return
	}
	s.n.checked.Add(1)
	err := s.read(ctx, key)
	if err == nil {
		s.n.ok.Add(1)
		return
	}
	if ctx.Err() != nil {
		return
	}
	s.n.broken.Add(1)
	if s.dryRun {
		fmt.Printf("✘ %s  %s  %v\n", f.ID, key, err)
		return
	}
	if rerr := rebuild(); rerr != nil {
		s.n.unrecoverable.Add(1)
		fmt.Printf("✘ %s  %s  %v; not repaired: %v %v\n", f.ID, key, err, rerr, f.Paths)
		return
	}
	if verr := s.read(ctx, key); verr != nil {
		s.n.unrecoverable.Add(1)
		fmt.Printf("✘ %s  %s  rewritten but still unreadable: %v\n", f.ID, key, verr)
		return
	}
	s.n.repaired.Add(1)
	fmt.Printf("✔ %s  %s  repaired (%v)\n", f.ID, key, err)
}

func (s *scrubber) read(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	obj, err := s.objects.Get(ctx, key)
	if err != nil {
		return err
	}
	defer obj.Body.Close()
	n, err := io.Copy(io.Discard, obj.Body)
	if err != nil {
		return err
	}
	if obj.Size > 0 && n != obj.Size {
		return fmt.Errorf("got %d of %d bytes", n, obj.Size)
	}
	return nil
}
