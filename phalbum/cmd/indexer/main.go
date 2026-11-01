// Command indexer takes jobs from the indexing queue, computes CLIP
// embeddings, stores thumbnails in SeaweedFS and saves photos in the library.
// It starts on new jobs as soon as the crawler queues them.
//
//	go run ./cmd/indexer [-once] [-store-originals]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/embedding"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/indexer"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/jobs"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/settings"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := settings.Load()
	if err != nil {
		return err
	}
	dsn := flag.String("db", "", "Postgres connection string (default: from settings)")
	once := flag.Bool("once", false, "process the queued jobs and exit instead of waiting for new ones")
	storeOriginals := flag.Bool("store-originals", cfg.StoreOriginals, "also copy original files to SeaweedFS (default from STORE_ORIGINALS)")
	lib := flag.String("lib", cfg.OnnxLib, "path to the ONNX Runtime shared library")
	models := flag.String("models", cfg.ModelsDir, "directory with the CLIP ONNX models")
	threads := flag.Int("threads", 0, "ONNX Runtime threads (0 lets it decide)")
	flag.Parse()
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
	jobStore, err := jobs.New(ctx, pool)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	photos, err := library.New(ctx, pool)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}

	objects, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey,
	})
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	if err := embedding.Init(*lib); err != nil {
		return fmt.Errorf("onnx runtime: %w", err)
	}
	defer embedding.Shutdown()
	clipCfg := embedding.DefaultConfig(*models)
	clipCfg.TextModel = "" // only images are embedded here
	clipCfg.Threads = *threads
	clip, err := embedding.New(clipCfg)
	if err != nil {
		return err
	}
	defer clip.Close()

	originals := "thumbnails only"
	if *storeOriginals {
		originals = "thumbnails + originals"
	}
	fmt.Fprintf(os.Stderr, "Indexer ready · model %s · %s → %s/%s\n",
		filepath.Base(*models), originals, cfg.S3Endpoint, cfg.S3Bucket)

	ix := &indexer.Indexer{
		Jobs: jobStore, Library: photos, Objects: objects, Embedder: clip,
		Model: filepath.Base(*models), StoreOriginals: *storeOriginals, Log: os.Stderr,
	}
	start := time.Now()
	stats, err := ix.Run(ctx, *once)
	fmt.Fprintf(os.Stderr, "✔ %d indexed, %d failed (%s)\n",
		stats.Indexed, stats.Failed, time.Since(start).Round(time.Millisecond))
	return err
}
