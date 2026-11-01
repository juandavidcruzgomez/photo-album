// Package indexer turns queued jobs into indexed photos: it computes the CLIP
// embedding, stores a thumbnail (and optionally the original) in object
// storage, and saves metadata and tags in the library.
package indexer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/embedding"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/imaging"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/jobs"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

const (
	thumbnailSize = 512
	// workingSize bounds the short side of the decoded copy used for the
	// embedding and the thumbnail; both need much less than a full photo.
	workingSize = thumbnailSize
	// staleAfter is how long a job may stay "processing" before it is
	// considered abandoned and queued again.
	staleAfter = 10 * time.Minute
	// pollInterval is a safety net in case a notification is missed.
	pollInterval = time.Minute
)

// Embedder computes image embeddings; *embedding.CLIP satisfies it.
type Embedder interface {
	EmbedImages(imgs ...image.Image) ([][]float32, error)
}

// Indexer processes indexing jobs.
type Indexer struct {
	Jobs           *jobs.Store
	Library        *library.Store
	Objects        storage.ObjectStore
	Embedder       Embedder
	Model          string // embedding model name, stored with each photo
	StoreOriginals bool
	Log            io.Writer // progress output; nil discards it
}

// Stats counts what a Run did.
type Stats struct {
	Indexed, Failed int
}

// Run processes jobs until ctx is cancelled. A job in progress is finished
// before returning. When once is true, Run returns as soon as the queue is
// empty instead of waiting for new jobs.
func (ix *Indexer) Run(ctx context.Context, once bool) (Stats, error) {
	var stats Stats
	if n, err := ix.Jobs.RequeueStale(ctx, staleAfter); err != nil {
		return stats, err
	} else if n > 0 {
		ix.logf("requeued %d abandoned jobs\n", n)
	}

	var listener *jobs.Listener
	defer func() {
		if listener != nil {
			listener.Close()
		}
	}()

	waiting := false
	for ctx.Err() == nil {
		job, err := ix.Jobs.Claim(ctx)
		if errors.Is(err, jobs.ErrNoJobs) {
			if once {
				return stats, nil
			}
			if !waiting {
				ix.logf("queue empty, waiting for jobs…\n")
				waiting = true
			}
			if listener == nil {
				if listener, err = ix.Jobs.Listen(ctx); err != nil {
					ix.logf("listen: %v (retrying)\n", err)
					sleep(ctx, 5*time.Second)
					continue
				}
			}
			if err := listener.Wait(ctx, pollInterval); err != nil && ctx.Err() == nil {
				ix.logf("listen: %v (reconnecting)\n", err)
				listener.Close()
				listener = nil
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			ix.logf("claim: %v (retrying)\n", err)
			sleep(ctx, 5*time.Second)
			continue
		}
		waiting = false

		// Finish the job even if shutdown is requested meanwhile.
		jctx := context.WithoutCancel(ctx)
		start := time.Now()
		if err := ix.Process(jctx, job); err != nil {
			stats.Failed++
			ix.logf("✖ %s: %v\n", job.Filename, err)
			if ferr := ix.Jobs.Fail(jctx, job.ID, err.Error()); ferr != nil {
				ix.logf("marking job %s failed: %v\n", job.ID, ferr)
			}
			continue
		}
		if err := ix.Jobs.Complete(jctx, job.ID); err != nil {
			ix.logf("marking job %s done: %v\n", job.ID, err)
		}
		stats.Indexed++
		ix.logf("✔ %s  %s  %s\n", job.Filename, humanSize(job.Size), time.Since(start).Round(time.Millisecond))
	}
	return stats, nil
}

// Process indexes a single job.
func (ix *Indexer) Process(ctx context.Context, job *jobs.Job) error {
	path, err := localPath(job.URI)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != job.Hash {
		return errors.New("content changed since crawl")
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if errors.Is(err, image.ErrFormat) {
		return fmt.Errorf("unsupported format: %s", job.Extension)
	}
	if err != nil {
		return fmt.Errorf("decoding: %w", err)
	}
	meta := readMetadata(data)
	work := imaging.Orient(imaging.Shrink(img, workingSize), meta.Orientation)
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if meta.Orientation >= 5 && meta.Orientation <= 8 {
		width, height = height, width
	}

	emb, err := ix.Embedder.EmbedImages(work)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	thumb, err := imaging.Thumbnail(work, thumbnailSize)
	if err != nil {
		return fmt.Errorf("thumbnail: %w", err)
	}
	thumbKey := storage.ThumbnailKey(job.Hash)
	if err := ix.Objects.Put(ctx, thumbKey, "image/jpeg", thumb); err != nil {
		return err
	}
	var originalKey string
	if ix.StoreOriginals {
		originalKey = storage.OriginalKey(job.Hash, job.Extension)
		if err := ix.Objects.Put(ctx, originalKey, job.MimeType, data); err != nil {
			return err
		}
	}

	_, err = ix.Library.SavePhoto(ctx, library.Photo{
		ContentHash:  job.Hash,
		OriginalKey:  originalKey,
		ThumbnailKey: thumbKey,
		Filename:     job.Filename,
		MimeType:     job.MimeType,
		Extension:    job.Extension,
		Size:         int64(len(data)),
		Width:        width,
		Height:       height,
		TakenAt:      meta.TakenAt,
		CameraMake:   meta.Make,
		CameraModel:  meta.Model,
		Latitude:     meta.Latitude,
		Longitude:    meta.Longitude,
		Exif:         job.Exif,
		Embedding:    emb[0],
		Model:        ix.Model,
	}, job.Tags)
	return err
}

var _ Embedder = (*embedding.CLIP)(nil)

func localPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("bad uri %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported uri scheme %q", u.Scheme)
	}
	return u.Path, nil
}

func (ix *Indexer) logf(format string, args ...any) {
	if ix.Log != nil {
		fmt.Fprintf(ix.Log, format, args...)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
