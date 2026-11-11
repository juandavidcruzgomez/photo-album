package repair

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
)

type putter map[string][]byte

func (p putter) Put(_ context.Context, key, _ string, body []byte) error {
	p[key] = body
	return nil
}

func TestRebuild(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 600, 1200)))
	sum := sha256.Sum256(buf.Bytes())
	good := filepath.Join(dir, "good.png")
	other := filepath.Join(dir, "other.png")
	os.WriteFile(good, buf.Bytes(), 0o644)
	os.WriteFile(other, []byte("edited since"), 0o644)

	f := &library.Files{
		ContentHash: hex.EncodeToString(sum[:]), ThumbnailKey: "thumbnails/x.jpg",
		OriginalKey: "originals/x.png", MimeType: "image/png",
		Paths: []string{filepath.Join(dir, "gone.png"), other, good},
	}
	p := putter{}
	thumb, err := RebuildThumbnail(context.Background(), p, f)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(p["thumbnails/x.jpg"]))
	if err != nil || !bytes.Equal(thumb, p["thumbnails/x.jpg"]) || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 512 {
		t.Fatalf("thumbnail: %v %v", err, img.Bounds())
	}
	if err := RebuildOriginal(context.Background(), p, f); err != nil || !bytes.Equal(p["originals/x.png"], buf.Bytes()) {
		t.Fatalf("original: %v", err)
	}

	f.Paths = []string{other}
	if _, err := ReadSource(f); !errors.Is(err, ErrChanged) {
		t.Errorf("changed: %v", err)
	}
	f.Paths = []string{filepath.Join(dir, "gone.png")}
	if _, err := ReadSource(f); !errors.Is(err, ErrNoSource) {
		t.Errorf("missing: %v", err)
	}
}
