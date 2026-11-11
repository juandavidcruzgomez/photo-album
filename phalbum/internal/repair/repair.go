// Package repair rebuilds objects in storage (thumbnails, copied originals)
// from the source files they came from, for when storage loses or corrupts
// them. Keys are content-addressed, so a rebuilt object goes back under the
// same key.
package repair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/imaging"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
)

var (
	// ErrNoSource means none of the photo's source paths can be read.
	ErrNoSource = errors.New("no readable source file")
	// ErrChanged means the source files exist but no longer hold the photo.
	ErrChanged = errors.New("source files changed since indexing")
)

// Putter writes objects; *storage.S3 satisfies it.
type Putter interface {
	Put(ctx context.Context, key, contentType string, body []byte) error
}

// ReadSource returns the contents of the first source path whose content
// still matches the photo's hash.
func ReadSource(f *library.Files) ([]byte, error) {
	readable := false
	for _, path := range f.Paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		readable = true
		sum := sha256.Sum256(data)
		if f.ContentHash == "" || hex.EncodeToString(sum[:]) == f.ContentHash {
			return data, nil
		}
	}
	if readable {
		return nil, ErrChanged
	}
	return nil, ErrNoSource
}

// Thumbnail builds the JPEG thumbnail of an original the way the indexer does.
func Thumbnail(original []byte) ([]byte, error) {
	prep, err := imaging.Prepare(original, imaging.ThumbnailSize)
	if err != nil {
		return nil, fmt.Errorf("decoding: %w", err)
	}
	return imaging.Thumbnail(prep.Image, imaging.ThumbnailSize)
}

// RebuildThumbnail regenerates a photo's thumbnail from its source and
// stores it again. It returns the thumbnail.
func RebuildThumbnail(ctx context.Context, p Putter, f *library.Files) ([]byte, error) {
	src, err := ReadSource(f)
	if err != nil {
		return nil, err
	}
	thumb, err := Thumbnail(src)
	if err != nil {
		return nil, err
	}
	if p != nil {
		if err := p.Put(ctx, f.ThumbnailKey, "image/jpeg", thumb); err != nil {
			return thumb, err
		}
	}
	return thumb, nil
}

// RebuildOriginal copies a photo's source file back into storage.
func RebuildOriginal(ctx context.Context, p Putter, f *library.Files) error {
	if f.OriginalKey == "" {
		return nil
	}
	src, err := ReadSource(f)
	if err != nil {
		return err
	}
	return p.Put(ctx, f.OriginalKey, f.MimeType, src)
}
