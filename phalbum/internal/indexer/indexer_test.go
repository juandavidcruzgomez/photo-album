package indexer

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/crawler"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/dbtest"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/jobs"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
)

type fakeEmbedder struct{}

func (fakeEmbedder) EmbedImages(imgs ...image.Image) ([][]float32, error) {
	out := make([][]float32, len(imgs))
	for i := range out {
		out[i] = make([]float32, 512)
		out[i][0] = 1
	}
	return out, nil
}

type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memStore) Put(_ context.Context, key, _ string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = body
	return nil
}

type env struct {
	ix   *Indexer
	pool *pgxpool.Pool
	objs *memStore
	dir  string
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.Pool(t)
	js, err := jobs.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	objs := &memStore{objects: map[string][]byte{}}
	return &env{
		ix:   &Indexer{Jobs: js, Library: lib, Objects: objs, Embedder: fakeEmbedder{}, Model: "fake"},
		pool: pool, objs: objs, dir: t.TempDir(),
	}
}

// enqueue writes a file and queues it the way the crawler does.
func (e *env) enqueue(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(e.dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := crawler.HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.ix.Jobs.Enqueue(context.Background(), jobs.NewJob{
		Hash: hash, Path: path, Size: int64(len(data)), ModTime: time.Now(),
		MimeType: "image/png", Extension: filepath.Ext(name)[1:], Tags: []string{"Holidays"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func pngBytes(t *testing.T, w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e *env) status(t *testing.T) map[string]string {
	rows, err := e.pool.Query(context.Background(), "SELECT filename, status || ':' || message FROM index_jobs")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for rows.Next() {
		var f, s string
		rows.Scan(&f, &s)
		out[f] = s
	}
	return out
}

func TestRunOnce(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.enqueue(t, "ok.png", pngBytes(t, 800, 600))
	e.enqueue(t, "fake.jpg", []byte("not an image"))
	changed := e.enqueue(t, "changed.png", pngBytes(t, 10, 10))
	os.WriteFile(changed, pngBytes(t, 20, 20), 0o644)

	stats, err := e.ix.Run(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Indexed != 1 || stats.Failed != 2 {
		t.Errorf("stats = %+v", stats)
	}

	st := e.status(t)
	if st["ok.png"] != "success:" || st["fake.jpg"] != "error:unsupported format: jpg" ||
		st["changed.png"] != "error:content changed since crawl" {
		t.Errorf("statuses = %v", st)
	}

	var w, h int
	var original *string
	var tag string
	err = e.pool.QueryRow(ctx, `
		SELECT width, height, original_key, tags.name FROM photos
		JOIN photo_tags ON photo_id = photos.id JOIN tags ON tags.id = tag_id`).Scan(&w, &h, &original, &tag)
	if err != nil {
		t.Fatal(err)
	}
	if w != 800 || h != 600 || original != nil || tag != "Holidays" {
		t.Errorf("photo: %dx%d original=%v tag=%s", w, h, original, tag)
	}
	if len(e.objs.objects) != 1 {
		t.Errorf("stored %d objects, want only the thumbnail", len(e.objs.objects))
	}
}

func TestStoreOriginals(t *testing.T) {
	e := setup(t)
	e.ix.StoreOriginals = true
	e.enqueue(t, "ok.png", pngBytes(t, 50, 40))
	if _, err := e.ix.Run(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if len(e.objs.objects) != 2 {
		t.Errorf("stored %d objects, want 2", len(e.objs.objects))
	}
	var original *string
	e.pool.QueryRow(context.Background(), "SELECT original_key FROM photos").Scan(&original)
	if original == nil {
		t.Error("original_key not set")
	}
}

func TestRunWakesOnNewJob(t *testing.T) {
	e := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan Stats)
	go func() {
		stats, _ := e.ix.Run(ctx, false)
		done <- stats
	}()
	time.Sleep(300 * time.Millisecond) // let it reach the waiting state
	e.enqueue(t, "late.png", pngBytes(t, 30, 30))

	deadline := time.Now().Add(5 * time.Second)
	for e.status(t)["late.png"] != "success:" {
		if time.Now().After(deadline) {
			t.Fatalf("job not indexed in time: %v", e.status(t))
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if stats := <-done; stats.Indexed != 1 {
		t.Errorf("stats = %+v", stats)
	}
}
