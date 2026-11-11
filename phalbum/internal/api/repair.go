package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/repair"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

const (
	// thumbnailTimeout bounds reading a thumbnail from storage. A corrupted
	// object makes SeaweedFS retry for many seconds before failing; past this
	// the thumbnail is rebuilt from the source instead.
	thumbnailTimeout = 3 * time.Second
	// originalTimeout bounds waiting for storage to start answering for an
	// original before falling back to the source file.
	originalTimeout = 5 * time.Second
	// maxRebuilds is how many thumbnails are decoded from source at once.
	// Each decode of a large photo takes tens of MB on a small machine.
	maxRebuilds = 2
	// repairTimeout bounds a background repair.
	repairTimeout = 2 * time.Minute
)

// repairs limits and de-duplicates the work of rebuilding objects.
type repairs struct {
	rebuild chan struct{} // semaphore for thumbnail rebuilds in requests
	bg      chan struct{} // one background original repair at a time
	running map[string]bool
}

func newRepairs() *repairs {
	return &repairs{
		rebuild: make(chan struct{}, maxRebuilds),
		bg:      make(chan struct{}, 1),
		running: map[string]bool{},
	}
}

// readThumbnail reads a whole thumbnail from storage, so a failure part-way
// through is still noticed before anything is sent.
func (s *Server) readThumbnail(ctx context.Context, key string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, thumbnailTimeout)
	defer cancel()
	obj, err := s.Objects.Get(ctx, key)
	if err != nil {
		return nil, "", err
	}
	defer obj.Body.Close()
	var buf bytes.Buffer
	if obj.Size > 0 {
		buf.Grow(int(obj.Size))
	}
	if _, err := io.Copy(&buf, obj.Body); err != nil {
		return nil, "", fmt.Errorf("reading %s: %w", key, err)
	}
	if obj.Size > 0 && int64(buf.Len()) != obj.Size {
		return nil, "", fmt.Errorf("reading %s: got %d of %d bytes", key, buf.Len(), obj.Size)
	}
	return buf.Bytes(), obj.ContentType, nil
}

// rebuildThumbnail regenerates a thumbnail from the photo's source file and
// stores it again, so the next request is served from storage.
func (s *Server) rebuildThumbnail(ctx context.Context, f *library.Files) ([]byte, error) {
	select {
	case s.repairs.rebuild <- struct{}{}:
		defer func() { <-s.repairs.rebuild }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	thumb, err := repair.RebuildThumbnail(context.WithoutCancel(ctx), nil, f)
	if err != nil {
		return nil, err
	}
	s.background(f.ThumbnailKey, func(ctx context.Context) error {
		return s.Objects.Put(ctx, f.ThumbnailKey, "image/jpeg", thumb)
	}, nil)
	return thumb, nil
}

// repairOriginal copies the source file back into storage in the background.
// It is skipped when another original is already being repaired; the scrub
// command finds whatever is left.
func (s *Server) repairOriginal(f *library.Files) {
	s.background(f.OriginalKey, func(ctx context.Context) error {
		return repair.RebuildOriginal(ctx, s.Objects, f)
	}, s.repairs.bg)
}

// background runs fn once per key at a time, holding sem (when non-nil)
// without waiting for it.
func (s *Server) background(key string, fn func(context.Context) error, sem chan struct{}) {
	if sem != nil {
		select {
		case sem <- struct{}{}:
		default:
			return
		}
	}
	s.repairMu.Lock()
	if s.repairs.running[key] {
		s.repairMu.Unlock()
		if sem != nil {
			<-sem
		}
		return
	}
	s.repairs.running[key] = true
	s.repairMu.Unlock()

	go func() {
		defer func() {
			s.repairMu.Lock()
			delete(s.repairs.running, key)
			s.repairMu.Unlock()
			if sem != nil {
				<-sem
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), repairTimeout)
		defer cancel()
		if err := fn(ctx); err != nil {
			s.logger().Printf("repair %s: %v", key, err)
			return
		}
		s.logger().Printf("repaired %s", key)
	}()
}

// sourceError maps a failure to rebuild from source to an API error.
func sourceError(err error) error {
	if errors.Is(err, repair.ErrNoSource) || errors.Is(err, repair.ErrChanged) {
		return storage.ErrNotFound
	}
	return err
}
