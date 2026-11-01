// Command crawler finds the images under a directory and queues an indexing
// job for every image whose content hasn't been seen before.
//
//	go run ./cmd/crawler [-db postgres://...] [-dry-run] <root>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/crawler"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/jobs"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/settings"
)

func main() {
	cfg, err := settings.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	dsn := flag.String("db", "", "Postgres connection string (default: from settings, i.e. $DATABASE_URL or $POSTGRES_*)")
	dryRun := flag.Bool("dry-run", false, "only list the images found, don't touch the database")
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	ctx := context.Background()
	var store *jobs.Store
	if !*dryRun {
		if *dsn == "" {
			*dsn = cfg.DSN()
		}
		if store, err = jobs.Open(ctx, *dsn); err != nil {
			fmt.Fprintln(os.Stderr, "database:", err)
			os.Exit(1)
		}
		defer store.Close()
	}

	status := newStatus(root)
	images, errs, err := crawler.Crawl(root, status.update)
	status.done()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *dryRun {
		for _, img := range images {
			fmt.Printf("%s\t%d\t%s\t%v\texif=%d\n", img.Path, img.Size, img.MimeType, img.Tags, len(img.Exif))
		}
	} else {
		errs = append(errs, enqueue(ctx, store, images, status)...)
	}

	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "error:", e)
	}
	status.summary(len(errs))
}

// enqueue queues a job for every image not already known. Files whose path,
// size and mtime match a previous crawl are skipped without hashing.
func enqueue(ctx context.Context, store *jobs.Store, images []crawler.Image, status *status) []error {
	known, err := store.KnownFiles(ctx)
	if err != nil {
		return []error{fmt.Errorf("loading known files: %w", err)}
	}

	var errs []error
	q := &status.queue
	q.total = len(images)
	for i, img := range images {
		q.done = i
		status.updateQueue(len(errs))

		if f, ok := known[img.Path]; ok && f.Unchanged(img.Size, img.ModTime) {
			q.unchanged++
			continue
		}
		hash, err := crawler.HashFile(img.Path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		created, err := store.Enqueue(ctx, jobs.NewJob{
			Hash: hash, Path: img.Path, Size: img.Size, ModTime: img.ModTime,
			MimeType: img.MimeType, Extension: img.Extension, Tags: img.Tags, Exif: img.Exif,
		})
		switch {
		case err != nil:
			errs = append(errs, err)
		case created:
			q.created++
		default:
			q.duplicates++
		}
	}
	q.done = len(images)
	status.done()
	return errs
}

// status renders a single, self-overwriting progress line on stderr while
// crawling and queuing. When stderr isn't a terminal it stays quiet until
// the summary.
type status struct {
	root  string
	tty   bool
	start time.Time
	last  time.Time
	frame int
	p     crawler.Progress
	queue queueProgress
}

type queueProgress struct {
	total, done                    int
	created, duplicates, unchanged int
}

var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func newStatus(root string) *status {
	tty := false
	if fi, err := os.Stderr.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	fmt.Fprintf(os.Stderr, "Crawling %s\n", abs)
	return &status{root: abs, tty: tty, start: time.Now()}
}

// tick reports whether it's time to redraw, and advances the spinner.
func (s *status) tick() bool {
	if !s.tty || time.Since(s.last) < 80*time.Millisecond {
		return false
	}
	s.last = time.Now()
	s.frame++
	return true
}

func (s *status) update(p crawler.Progress) {
	s.p = p
	if !s.tick() {
		return
	}
	dir, err := filepath.Rel(s.root, p.Dir)
	if err != nil {
		dir = p.Dir
	}
	fmt.Fprintf(os.Stderr, "\r\033[K%c %d dirs · %d files · %d images · %d errors  %s",
		spinner[s.frame%len(spinner)], p.Dirs, p.Files, p.Images, p.Errors, shorten(dir, 50))
}

func (s *status) updateQueue(errors int) {
	if !s.tick() {
		return
	}
	q := s.queue
	fmt.Fprintf(os.Stderr, "\r\033[K%c queuing %d/%d · %d new · %d dup · %d unchanged · %d errors",
		spinner[s.frame%len(spinner)], q.done, q.total, q.created, q.duplicates, q.unchanged, errors)
}

// done clears the progress line so regular output starts on a clean line.
func (s *status) done() {
	if s.tty {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

func (s *status) summary(errors int) {
	fmt.Fprintf(os.Stderr, "✔ %d images in %d files across %d dirs, %d errors (%s)\n",
		s.p.Images, s.p.Files, s.p.Dirs, errors, time.Since(s.start).Round(time.Millisecond))
	if q := s.queue; q.total > 0 {
		fmt.Fprintf(os.Stderr, "  %d new jobs, %d duplicates, %d unchanged\n", q.created, q.duplicates, q.unchanged)
	}
}

// shorten keeps the tail of a path, which is the most informative part.
func shorten(path string, max int) string {
	r := []rune(path)
	if len(r) <= max {
		return path
	}
	return "…" + string(r[len(r)-max+1:])
}
