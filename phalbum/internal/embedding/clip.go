// Package embedding computes CLIP image and text embeddings with ONNX Runtime.
package embedding

import (
	"errors"
	"fmt"
	"image"
	"math"
	"path/filepath"

	ort "github.com/yalue/onnxruntime_go"
)

// Dim is the size of the shared image/text embedding space.
const Dim = 512

// Init loads the ONNX Runtime shared library. It must be called once before
// creating any encoder.
func Init(sharedLibPath string) error {
	if ort.IsInitialized() {
		return nil
	}
	ort.SetSharedLibraryPath(sharedLibPath)
	return ort.InitializeEnvironment()
}

// Shutdown releases the ONNX Runtime environment.
func Shutdown() error {
	return ort.DestroyEnvironment()
}

// Config locates the CLIP model files. Either encoder can be left out: the
// indexer only needs the vision model and the web server only the text model.
type Config struct {
	VisionModel string
	TextModel   string
	Vocab       string
	Merges      string
	// Threads used by ONNX Runtime for each session; 0 lets ORT decide.
	Threads int
}

// DefaultConfig returns the file layout produced by scripts/fetch-models.sh.
func DefaultConfig(modelDir string) Config {
	return Config{
		VisionModel: filepath.Join(modelDir, "vision_model_quantized.onnx"),
		TextModel:   filepath.Join(modelDir, "text_model_quantized.onnx"),
		Vocab:       filepath.Join(modelDir, "vocab.json"),
		Merges:      filepath.Join(modelDir, "merges.txt"),
	}
}

// CLIP embeds images and text into the same vector space.
type CLIP struct {
	vision    *ort.DynamicAdvancedSession
	text      *ort.DynamicAdvancedSession
	tokenizer *Tokenizer
}

// New creates the sessions described by cfg.
func New(cfg Config) (*CLIP, error) {
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy()
	if cfg.Threads > 0 {
		if err := opts.SetIntraOpNumThreads(cfg.Threads); err != nil {
			return nil, err
		}
	}

	c := &CLIP{}
	if cfg.VisionModel != "" {
		c.vision, err = ort.NewDynamicAdvancedSession(cfg.VisionModel,
			[]string{"pixel_values"}, []string{"image_embeds"}, opts)
		if err != nil {
			return nil, fmt.Errorf("loading vision model: %w", err)
		}
	}
	if cfg.TextModel != "" {
		c.text, err = ort.NewDynamicAdvancedSession(cfg.TextModel,
			[]string{"input_ids"}, []string{"text_embeds"}, opts)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("loading text model: %w", err)
		}
		c.tokenizer, err = NewTokenizer(cfg.Vocab, cfg.Merges)
		if err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

// Close releases the ONNX Runtime sessions.
func (c *CLIP) Close() {
	if c.vision != nil {
		c.vision.Destroy()
	}
	if c.text != nil {
		c.text.Destroy()
	}
}

// EmbedImages returns one L2-normalized embedding per image.
func (c *CLIP) EmbedImages(imgs ...image.Image) ([][]float32, error) {
	if c.vision == nil {
		return nil, errors.New("vision model not loaded")
	}
	n := len(imgs)
	pixels := make([]float32, 0, n*3*ImageSize*ImageSize)
	for _, img := range imgs {
		pixels = append(pixels, Preprocess(img)...)
	}
	input, err := ort.NewTensor(ort.NewShape(int64(n), 3, ImageSize, ImageSize), pixels)
	if err != nil {
		return nil, err
	}
	defer input.Destroy()
	return run(c.vision, input, n)
}

// EmbedTexts returns one L2-normalized embedding per text.
func (c *CLIP) EmbedTexts(texts ...string) ([][]float32, error) {
	if c.text == nil {
		return nil, errors.New("text model not loaded")
	}
	n := len(texts)
	encoded := make([][]int64, n)
	seqLen := 0
	for i, t := range texts {
		encoded[i] = c.tokenizer.Encode(t)
		seqLen = max(seqLen, len(encoded[i]))
	}
	// Pad with the end token: the model pools the first end token, and the
	// causal mask keeps padding after it from affecting the result.
	ids := make([]int64, n*seqLen)
	for i, e := range encoded {
		row := ids[i*seqLen : (i+1)*seqLen]
		copy(row, e)
		for j := len(e); j < seqLen; j++ {
			row[j] = c.tokenizer.endID
		}
	}
	input, err := ort.NewTensor(ort.NewShape(int64(n), int64(seqLen)), ids)
	if err != nil {
		return nil, err
	}
	defer input.Destroy()
	return run(c.text, input, n)
}

func run(s *ort.DynamicAdvancedSession, input ort.Value, n int) ([][]float32, error) {
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(int64(n), Dim))
	if err != nil {
		return nil, err
	}
	defer output.Destroy()
	if err := s.Run([]ort.Value{input}, []ort.Value{output}); err != nil {
		return nil, err
	}
	data := output.GetData()
	result := make([][]float32, n)
	for i := range result {
		v := make([]float32, Dim)
		copy(v, data[i*Dim:(i+1)*Dim])
		normalize(v)
		result[i] = v
	}
	return result, nil
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// Cosine returns the cosine similarity of two L2-normalized embeddings.
func Cosine(a, b []float32) float32 {
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}
