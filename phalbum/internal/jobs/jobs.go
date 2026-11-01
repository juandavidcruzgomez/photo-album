// Package jobs is the Postgres-backed indexing queue shared by the crawler,
// which enqueues new images, and the indexer, which claims and processes them
// at its own pace.
package jobs

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/db"
)

//go:embed schema.sql
var schema string

// Job statuses.
const (
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusSuccess    = "success"
	StatusError      = "error"
)

// ErrNoJobs is returned by Claim when nothing is queued.
var ErrNoJobs = errors.New("no queued jobs")

// Store wraps the connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres and makes sure the schema exists. The Store owns
// the pool; Close releases it.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	s, err := New(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// New uses an existing pool, making sure the schema exists. Closing the pool
// is left to the caller.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := db.ApplySchema(ctx, pool, schema); err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() {
	s.pool.Close()
}

// FileState is what the crawler last recorded for a path.
type FileState struct {
	Size    int64
	ModTime time.Time
	Hash    string
}

// Unchanged reports whether a file with this size and mtime can be assumed
// to have the same content as the recorded one.
func (f FileState) Unchanged(size int64, modTime time.Time) bool {
	return f.Size == size && f.ModTime.Equal(dbTime(modTime))
}

// dbTime truncates to the microsecond precision Postgres stores.
func dbTime(t time.Time) time.Time {
	return t.Truncate(time.Microsecond)
}

// KnownFiles returns every path the crawler has recorded.
func (s *Store) KnownFiles(ctx context.Context) (map[string]FileState, error) {
	rows, err := s.pool.Query(ctx, `SELECT path, size, mod_time, content_hash FROM crawled_files`)
	if err != nil {
		return nil, err
	}
	known := map[string]FileState{}
	var path string
	var f FileState
	_, err = pgx.ForEachRow(rows, []any{&path, &f.Size, &f.ModTime, &f.Hash}, func() error {
		known[path] = f
		return nil
	})
	return known, err
}

// NewJob is an image found by the crawler.
type NewJob struct {
	Hash      string
	Path      string // absolute local path
	Size      int64
	ModTime   time.Time
	MimeType  string
	Extension string
	Tags      []string
	Exif      map[string]string
}

// Enqueue records the file and queues an indexing job for its content unless
// one already exists. created reports whether a new job was queued.
func (s *Store) Enqueue(ctx context.Context, j NewJob) (created bool, err error) {
	var exif []byte
	if j.Exif != nil {
		if exif, err = json.Marshal(j.Exif); err != nil {
			return false, err
		}
	}
	tags := j.Tags
	if tags == nil {
		tags = []string{}
	}
	uri := (&url.URL{Scheme: "file", Path: j.Path}).String()

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO crawled_files (path, size, mod_time, content_hash)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (path) DO UPDATE
			SET size = EXCLUDED.size, mod_time = EXCLUDED.mod_time,
			    content_hash = EXCLUDED.content_hash, seen_at = now()`,
			j.Path, j.Size, dbTime(j.ModTime), j.Hash)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO index_jobs (content_hash, uri, filename, size, mime_type, extension, tags, exif)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (content_hash) DO NOTHING`,
			j.Hash, uri, filepath.Base(j.Path), j.Size, j.MimeType, j.Extension, tags, exif)
		if err != nil {
			return err
		}
		created = tag.RowsAffected() == 1
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("%s: %w", j.Path, err)
	}
	return created, nil
}

// Job is an indexing job claimed by the indexer.
type Job struct {
	ID        string
	Hash      string
	URI       string
	Filename  string
	MimeType  string
	Extension string
	Size      int64
	Tags      []string
	Exif      map[string]string
	Attempts  int
	CreatedAt time.Time
}

// Claim takes the oldest queued job and marks it processing. It is safe to
// call from several indexers at once: each job is handed out only once.
// Returns ErrNoJobs when the queue is empty.
func (s *Store) Claim(ctx context.Context) (*Job, error) {
	var j Job
	var exif []byte
	err := s.pool.QueryRow(ctx, `
		UPDATE index_jobs
		SET status = 'processing', attempts = attempts + 1, updated_at = now()
		WHERE job_id = (
			SELECT job_id FROM index_jobs
			WHERE status = 'queued'
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING job_id::text, content_hash, uri, filename, mime_type, extension, size, tags, exif, attempts, created_at`,
	).Scan(&j.ID, &j.Hash, &j.URI, &j.Filename, &j.MimeType, &j.Extension, &j.Size, &j.Tags, &exif, &j.Attempts, &j.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoJobs
	}
	if err != nil {
		return nil, err
	}
	if exif != nil {
		if err := json.Unmarshal(exif, &j.Exif); err != nil {
			return nil, fmt.Errorf("job %s: decoding exif: %w", j.ID, err)
		}
	}
	return &j, nil
}

// Complete marks a job as successfully indexed.
func (s *Store) Complete(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, StatusSuccess, "")
}

// Fail marks a job as failed with a message.
func (s *Store) Fail(ctx context.Context, id, msg string) error {
	return s.setStatus(ctx, id, StatusError, msg)
}

func (s *Store) setStatus(ctx context.Context, id, status, msg string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE index_jobs SET status = $2, message = $3, updated_at = now() WHERE job_id = $1`,
		id, status, msg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job %s not found", id)
	}
	return nil
}

// RequeueStale puts back jobs that have been processing for longer than
// olderThan, e.g. because the indexer crashed. It returns how many moved.
func (s *Store) RequeueStale(ctx context.Context, olderThan time.Duration) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE index_jobs SET status = 'queued', updated_at = now()
		WHERE status = 'processing' AND updated_at < now() - make_interval(secs => $1)`,
		olderThan.Seconds())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// Listener receives a notification whenever jobs are queued.
type Listener struct {
	conn *pgx.Conn
}

// Listen opens a dedicated connection subscribed to new-job notifications.
func (s *Store) Listen(ctx context.Context) (*Listener, error) {
	c, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	conn := c.Hijack()
	if _, err := conn.Exec(ctx, "LISTEN index_jobs"); err != nil {
		conn.Close(ctx)
		return nil, err
	}
	return &Listener{conn: conn}, nil
}

// Wait blocks until jobs are queued or timeout passes, whichever comes
// first; either way the caller should check the queue. It returns an error
// only when ctx is done or the connection broke.
func (l *Listener) Wait(ctx context.Context, timeout time.Duration) error {
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := l.conn.WaitForNotification(wctx)
	if err != nil && ctx.Err() == nil && errors.Is(wctx.Err(), context.DeadlineExceeded) {
		return nil
	}
	return err
}

// Close closes the listener's connection.
func (l *Listener) Close() {
	l.conn.Close(context.Background())
}
