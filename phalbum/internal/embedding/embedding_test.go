package embedding

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Tests that need the model files skip unless scripts/fetch-models.sh was run.
var (
	modelDir = filepath.Join("..", "..", "models", "clip-vit-base-patch32")
	ortLib   = filepath.Join("..", "..", "models", "libonnxruntime.so.1.31.0")
)

func requireFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not found; run scripts/fetch-models.sh", path)
	}
}

func TestTokenizerEncode(t *testing.T) {
	cfg := DefaultConfig(modelDir)
	requireFile(t, cfg.Vocab)
	tok, err := NewTokenizer(cfg.Vocab, cfg.Merges)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		text string
		want []int64
	}{
		{"a photo of a cat", []int64{49406, 320, 1125, 539, 320, 2368, 49407}},
		{"  A   Photo of a CAT ", []int64{49406, 320, 1125, 539, 320, 2368, 49407}},
		{"Hello, world!", []int64{49406, 3306, 267, 1002, 256, 49407}},
	}
	for _, tt := range tests {
		if got := tok.Encode(tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("Encode(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}

	long := tok.Encode(strings.Repeat("cat ", 200))
	if len(long) != ContextLength || long[len(long)-1] != tok.endID {
		t.Errorf("long input: len %d, last %d", len(long), long[len(long)-1])
	}
}

func TestPreprocessShape(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	out := Preprocess(img)
	if len(out) != 3*ImageSize*ImageSize {
		t.Fatalf("len = %d", len(out))
	}
	// White pixels normalize to (1-mean)/std on every channel.
	for c := 0; c < 3; c++ {
		want := (1 - clipMean[c]) / clipStd[c]
		if got := out[c*ImageSize*ImageSize]; abs(got-want) > 1e-4 {
			t.Errorf("channel %d = %f, want %f", c, got, want)
		}
	}
}

func TestTextEmbeddingsAreConsistent(t *testing.T) {
	cfg := DefaultConfig(modelDir)
	requireFile(t, ortLib)
	requireFile(t, cfg.TextModel)
	if err := Init(ortLib); err != nil {
		t.Fatal(err)
	}
	cfg.VisionModel = ""
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Batched (padded) and single runs must agree.
	batch, err := c.EmbedTexts("a cat", "a photo of a dog on the beach")
	if err != nil {
		t.Fatal(err)
	}
	single, err := c.EmbedTexts("a cat")
	if err != nil {
		t.Fatal(err)
	}
	if sim := Cosine(batch[0], single[0]); sim < 0.999 {
		t.Errorf("padded vs unpadded similarity = %f", sim)
	}
	if n := Cosine(single[0], single[0]); abs(n-1) > 1e-4 {
		t.Errorf("embedding not normalized: %f", n)
	}
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
