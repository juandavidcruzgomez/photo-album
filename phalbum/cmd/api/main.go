// Command api serves the search API used by the web app.
//
//	go run ./cmd/api [-addr :8090]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/api"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/embedding"
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
	addr := flag.String("addr", cfg.APIAddr, "listen address (default from API_ADDR)")
	dsn := flag.String("db", "", "Postgres connection string (default: from settings)")
	lib := flag.String("lib", cfg.OnnxLib, "path to the ONNX Runtime shared library")
	models := flag.String("models", cfg.ModelsDir, "directory with the CLIP ONNX models and tokenizer files")
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
	clip, err := embedding.New(embedding.DefaultConfig(*models))
	if err != nil {
		return err
	}
	defer clip.Close()

	srv := &http.Server{
		Addr: *addr,
		Handler: (&api.Server{
			Library: photos, Embedder: clip, Objects: objects,
			Ping: pool.Ping, CORSOrigins: cfg.CORSOrigins,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("search API listening on %s", *addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
