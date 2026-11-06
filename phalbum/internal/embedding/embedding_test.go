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
	mclipDir = filepath.Join("..", "..", "models", "clip-ViT-B-32-multilingual-v1")
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

func TestWordPieceEncode(t *testing.T) {
	cfg := Config{}.WithMultilingualText(mclipDir)
	requireFile(t, cfg.Vocab)
	tok, err := NewWordPiece(cfg.Vocab)
	if err != nil {
		t.Fatal(err)
	}
	// Expected ids come from the Hugging Face tokenizer.
	tests := []struct {
		text string
		want []int64
	}{
		{"a photo of a cat", []int64{101, 169, 38171, 10108, 169, 41163, 102}},
		{"une photo d'un chat", []int64{101, 10231, 38171, 172, 112, 10119, 69338, 102}},
		{"Été à Paris!", []int64{101, 234, 12208, 254, 10728, 106, 102}},
		{"  Un CHIEN   sur la plage\t", []int64{101, 11038, 39774, 71655, 11537, 10326, 10109, 98482, 102}},
		{"北京的猫", []int64{101, 2650, 2172, 5718, 5453, 102}},
		{"coucher de soleil, Montréal (2019)", []int64{101, 95930, 10129, 10104, 61879, 117, 24554, 113, 19493, 114, 102}},
		{"xyzzyqwvmplk", []int64{101, 192, 10157, 68471, 11703, 10874, 10477, 18573, 24323, 102}},
		// Decomposed accents are normalized to NFC first.
		{"E\u0301te\u0301 a\u0300 Paris!", []int64{101, 234, 12208, 254, 10728, 106, 102}},
	}
	for _, tt := range tests {
		if got := tok.Encode(tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("Encode(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}

	long := tok.Encode(strings.Repeat("chat ", 200))
	if len(long) != WordPieceMaxLength || long[len(long)-1] != tok.sepID {
		t.Errorf("long input: len %d, last %d", len(long), long[len(long)-1])
	}
}

func TestMultilingualTextEmbeddings(t *testing.T) {
	cfg := Config{}.WithMultilingualText(mclipDir)
	requireFile(t, ortLib)
	requireFile(t, cfg.TextModel)
	if err := Init(ortLib); err != nil {
		t.Fatal(err)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	embs, err := c.EmbedTexts("a photo of a cat", "une photo d'un chat", "a red car")
	if err != nil {
		t.Fatal(err)
	}
	// First values of the unquantized sentence-transformers model's output.
	want := []float32{0.01095, -0.00008, -0.02707, -0.01806, -0.01118, 0.0263, -0.02398, -0.06927}
	for i, w := range want {
		if abs(embs[0][i]-w) > 0.01 {
			t.Errorf("embedding[%d] = %f, want about %f", i, embs[0][i], w)
		}
	}
	cat, chat, car := embs[0], embs[1], embs[2]
	if sim := Cosine(cat, chat); sim < 0.95 || sim <= Cosine(cat, car) {
		t.Errorf("cat/chat similarity = %f, cat/car = %f", sim, Cosine(cat, car))
	}

	// Batched (padded) and single runs must agree.
	single, err := c.EmbedTexts("a red car")
	if err != nil {
		t.Fatal(err)
	}
	if sim := Cosine(car, single[0]); sim < 0.999 {
		t.Errorf("padded vs unpadded similarity = %f", sim)
	}
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
