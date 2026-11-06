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

// multilingualHidden is the hidden size of the multilingual DistilBERT encoder.
const multilingualHidden = 768

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

// TextKind selects the text encoder architecture.
type TextKind int

const (
	// TextCLIP is OpenAI CLIP's English text encoder with its BPE tokenizer.
	TextCLIP TextKind = iota
	// TextMultilingual is sentence-transformers' clip-ViT-B-32-multilingual-v1:
	// a multilingual DistilBERT distilled into the CLIP ViT-B/32 space, so its
	// embeddings match the existing image embeddings.
	TextMultilingual
)

// Config locates the CLIP model files. Either encoder can be left out: the
// indexer only needs the vision model and the web server only the text model.
type Config struct {
	VisionModel string
	TextKind    TextKind
	TextModel   string
	// Vocab is vocab.json for TextCLIP and vocab.txt for TextMultilingual.
	Vocab  string
	Merges string // TextCLIP only
	Dense  string // TextMultilingual only: the 768→512 projection weights
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

// WithMultilingualText returns cfg with the text encoder replaced by the
// multilingual one in modelDir, laid out by scripts/fetch-models.sh.
func (cfg Config) WithMultilingualText(modelDir string) Config {
	cfg.TextKind = TextMultilingual
	cfg.TextModel = filepath.Join(modelDir, "text_model_quantized.onnx")
	cfg.Vocab = filepath.Join(modelDir, "vocab.txt")
	cfg.Merges = ""
	cfg.Dense = filepath.Join(modelDir, "dense.safetensors")
	return cfg
}

// Files returns the model files cfg needs.
func (cfg Config) Files() []string {
	var files []string
	if cfg.VisionModel != "" {
		files = append(files, cfg.VisionModel)
	}
	if cfg.TextModel != "" {
		files = append(files, cfg.TextModel, cfg.Vocab)
		if cfg.Merges != "" {
			files = append(files, cfg.Merges)
		}
		if cfg.Dense != "" {
			files = append(files, cfg.Dense)
		}
	}
	return files
}

// CLIP embeds images and text into the same vector space.
type CLIP struct {
	vision    *ort.DynamicAdvancedSession
	text      *ort.DynamicAdvancedSession
	tokenizer *Tokenizer
	// Multilingual text encoder: tokenizer and Dense weights, [Dim][multilingualHidden] row-major.
	wordPiece *WordPiece
	dense     []float32
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
		if err := c.loadText(cfg, opts); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

func (c *CLIP) loadText(cfg Config, opts *ort.SessionOptions) error {
	var err error
	switch cfg.TextKind {
	case TextCLIP:
		c.text, err = ort.NewDynamicAdvancedSession(cfg.TextModel,
			[]string{"input_ids"}, []string{"text_embeds"}, opts)
		if err != nil {
			return fmt.Errorf("loading text model: %w", err)
		}
		c.tokenizer, err = NewTokenizer(cfg.Vocab, cfg.Merges)
		return err
	case TextMultilingual:
		c.text, err = ort.NewDynamicAdvancedSession(cfg.TextModel,
			[]string{"input_ids", "attention_mask"}, []string{"last_hidden_state"}, opts)
		if err != nil {
			return fmt.Errorf("loading text model: %w", err)
		}
		if c.wordPiece, err = NewWordPiece(cfg.Vocab); err != nil {
			return err
		}
		var shape []int
		c.dense, shape, err = readSafetensor(cfg.Dense, "linear.weight")
		if err != nil {
			return fmt.Errorf("loading dense weights: %w", err)
		}
		if len(shape) != 2 || shape[0] != Dim || shape[1] != multilingualHidden {
			return fmt.Errorf("dense weights have shape %v, want [%d %d]", shape, Dim, multilingualHidden)
		}
		return nil
	default:
		return fmt.Errorf("unknown text model kind %d", cfg.TextKind)
	}
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
	if c.wordPiece != nil {
		return c.embedTextsMultilingual(texts)
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

// embedTextsMultilingual runs the transformer, then the sentence-transformers
// head: mean pooling over real tokens and a bias-free Dense projection.
func (c *CLIP) embedTextsMultilingual(texts []string) ([][]float32, error) {
	n := len(texts)
	encoded := make([][]int64, n)
	seqLen := 0
	for i, t := range texts {
		encoded[i] = c.wordPiece.Encode(t)
		seqLen = max(seqLen, len(encoded[i]))
	}
	ids := make([]int64, n*seqLen)
	mask := make([]int64, n*seqLen)
	for i, e := range encoded {
		for j := range seqLen {
			if j < len(e) {
				ids[i*seqLen+j], mask[i*seqLen+j] = e[j], 1
			} else {
				ids[i*seqLen+j] = c.wordPiece.padID
			}
		}
	}
	shape := ort.NewShape(int64(n), int64(seqLen))
	idsTensor, err := ort.NewTensor(shape, ids)
	if err != nil {
		return nil, err
	}
	defer idsTensor.Destroy()
	maskTensor, err := ort.NewTensor(shape, mask)
	if err != nil {
		return nil, err
	}
	defer maskTensor.Destroy()
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(int64(n), int64(seqLen), multilingualHidden))
	if err != nil {
		return nil, err
	}
	defer output.Destroy()
	if err := c.text.Run([]ort.Value{idsTensor, maskTensor}, []ort.Value{output}); err != nil {
		return nil, err
	}

	hidden := output.GetData()
	result := make([][]float32, n)
	pooled := make([]float32, multilingualHidden)
	for i, e := range encoded {
		clear(pooled)
		for j := range e {
			tok := hidden[(i*seqLen+j)*multilingualHidden : (i*seqLen+j+1)*multilingualHidden]
			for h, x := range tok {
				pooled[h] += x
			}
		}
		inv := 1 / float32(len(e))
		v := make([]float32, Dim)
		for k := range v {
			row := c.dense[k*multilingualHidden : (k+1)*multilingualHidden]
			var dot float32
			for h, x := range pooled {
				dot += row[h] * x
			}
			v[k] = dot * inv
		}
		normalize(v)
		result[i] = v
	}
	return result, nil
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
