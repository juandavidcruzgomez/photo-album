// Command api serves the search API and the web app.
//
//	go run ./cmd/api [-addr :8090]
//	api -healthcheck   # exit 0 if the API at -addr answers /api/health (for Docker)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
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
	"github.com/juandavidcruzgomez/photo-album/phalbum/web"
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
	textModels := flag.String("text-models", cfg.TextModelsDir, "directory with the multilingual text model; empty uses CLIP's English text model")
	healthcheck := flag.Bool("healthcheck", false, "check that the API running at -addr is healthy, then exit")
	flag.Parse()
	if *healthcheck {
		return checkHealth(*addr)
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
	if *textModels != "" {
		clipCfg = clipCfg.WithMultilingualText(*textModels)
	}
	clip, err := embedding.New(clipCfg)
	if err != nil {
		return err
	}
	defer clip.Close()

	srv := &http.Server{
		Addr: *addr,
		Handler: (&api.Server{
			Library: photos, Embedder: clip, Objects: objects, Static: web.Handler(),
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

// checkHealth asks a running API for its health, so container images need
// no curl.
func checkHealth(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/api/health")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: %s", resp.Status)
	}
	return nil
}
