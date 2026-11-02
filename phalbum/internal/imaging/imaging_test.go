package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

var red = color.RGBA{255, 0, 0, 255}

// marked returns a 3×2 image whose top-left pixel is red.
func marked() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.SetRGBA(0, 0, red)
	return img
}

func TestOrient(t *testing.T) {
	// Where the stored top-left pixel ends up once the image is upright.
	cases := map[int]struct{ w, h, x, y int }{
		1: {3, 2, 0, 0},
		2: {3, 2, 2, 0},
		3: {3, 2, 2, 1},
		4: {3, 2, 0, 1},
		5: {2, 3, 0, 0},
		6: {2, 3, 1, 0},
		7: {2, 3, 1, 2},
		8: {2, 3, 0, 2},
	}
	for o, want := range cases {
		got := Orient(marked(), o)
		if got.Bounds().Dx() != want.w || got.Bounds().Dy() != want.h {
			t.Errorf("orientation %d: size %v, want %dx%d", o, got.Bounds().Size(), want.w, want.h)
			continue
		}
		if got.RGBAAt(want.x, want.y) != red {
			t.Errorf("orientation %d: red pixel not at (%d,%d)", o, want.x, want.y)
		}
	}
}

func TestShrink(t *testing.T) {
	got := Shrink(image.NewRGBA(image.Rect(0, 0, 2000, 1000)), 500)
	if got.Bounds().Dx() != 1000 || got.Bounds().Dy() != 500 {
		t.Errorf("Shrink size %v", got.Bounds().Size())
	}
	small := Shrink(image.NewRGBA(image.Rect(0, 0, 40, 30)), 500)
	if small.Bounds().Dx() != 40 {
		t.Errorf("small image resized to %v", small.Bounds().Size())
	}
}

func TestThumbnail(t *testing.T) {
	data, err := Thumbnail(image.NewRGBA(image.Rect(0, 0, 1000, 600)), 512)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 307 {
		t.Errorf("thumbnail size %v", b.Size())
	}
}

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPrepareFallsBackToEmbeddedPreview(t *testing.T) {
	// A JPEG whose main image data is cut short, followed by a tiny
	// thumbnail and a larger preview, like the broken scans in the wild.
	main := encodeJPEG(t, 400, 300)
	broken := append([]byte{}, main[:len(main)/2]...)
	broken = append(broken, encodeJPEG(t, 16, 12)...)
	broken = append(broken, encodeJPEG(t, 64, 48)...)

	p, err := Prepare(broken, EmbedSize)
	if err != nil {
		t.Fatal(err)
	}
	if !p.FromPreview || p.Width != 64 || p.Height != 48 {
		t.Errorf("got %dx%d fromPreview=%v, want the 64x48 preview", p.Width, p.Height, p.FromPreview)
	}

	ok, err := Prepare(main, EmbedSize)
	if err != nil || ok.FromPreview || ok.Width != 400 {
		t.Errorf("intact JPEG: %+v %v", ok, err)
	}

	if _, err := Prepare(main[:len(main)/2], EmbedSize); err == nil {
		t.Error("broken JPEG without a preview should fail")
	}
	if _, err := Prepare([]byte("not an image"), EmbedSize); err == nil {
		t.Error("non-image should fail")
	}
}
