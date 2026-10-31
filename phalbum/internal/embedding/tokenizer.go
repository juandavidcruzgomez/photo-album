package embedding

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"os"
	"regexp"
	"strings"
)

const (
	// ContextLength is the fixed sequence length CLIP's text encoder was trained with.
	ContextLength = 77

	startToken = "<|startoftext|>"
	endToken   = "<|endoftext|>"
)

var (
	// Same pattern as OpenAI's simple_tokenizer, applied to lowercased text.
	tokenPattern = regexp.MustCompile(`<\|startoftext\|>|<\|endoftext\|>|'s|'t|'re|'ve|'m|'ll|'d|\p{L}+|\p{N}|[^\s\p{L}\p{N}]+`)
	whitespace   = regexp.MustCompile(`\s+`)
)

// Tokenizer is a pure Go implementation of CLIP's byte-level BPE tokenizer.
type Tokenizer struct {
	encoder     map[string]int64
	bpeRanks    map[[2]string]int
	byteEncoder [256]string
	cache       map[string][]string
	startID     int64
	endID       int64
}

// NewTokenizer loads a CLIP tokenizer from Hugging Face style vocab.json and merges.txt files.
func NewTokenizer(vocabPath, mergesPath string) (*Tokenizer, error) {
	vocabData, err := os.ReadFile(vocabPath)
	if err != nil {
		return nil, fmt.Errorf("reading vocab: %w", err)
	}
	t := &Tokenizer{
		bpeRanks:    make(map[[2]string]int),
		byteEncoder: bytesToUnicode(),
		cache:       make(map[string][]string),
	}
	if err := json.Unmarshal(vocabData, &t.encoder); err != nil {
		return nil, fmt.Errorf("parsing vocab: %w", err)
	}

	f, err := os.Open(mergesPath)
	if err != nil {
		return nil, fmt.Errorf("reading merges: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	rank := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#version") || line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid merge line %q", line)
		}
		t.bpeRanks[[2]string{parts[0], parts[1]}] = rank
		rank++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading merges: %w", err)
	}

	var ok bool
	if t.startID, ok = t.encoder[startToken]; !ok {
		return nil, fmt.Errorf("vocab is missing %s", startToken)
	}
	if t.endID, ok = t.encoder[endToken]; !ok {
		return nil, fmt.Errorf("vocab is missing %s", endToken)
	}
	return t, nil
}

// Encode returns the token ids for text wrapped in start/end tokens, truncated
// to ContextLength. The result is not padded.
func (t *Tokenizer) Encode(text string) []int64 {
	text = html.UnescapeString(html.UnescapeString(text))
	text = strings.ToLower(strings.TrimSpace(whitespace.ReplaceAllString(text, " ")))

	ids := []int64{t.startID}
	for _, token := range tokenPattern.FindAllString(text, -1) {
		var b strings.Builder
		for _, c := range []byte(token) {
			b.WriteString(t.byteEncoder[c])
		}
		for _, piece := range t.bpe(b.String()) {
			if id, ok := t.encoder[piece]; ok {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) > ContextLength-1 {
		ids = ids[:ContextLength-1]
	}
	return append(ids, t.endID)
}

// bpe splits a byte-encoded token into vocabulary pieces by repeatedly merging
// the lowest ranked adjacent pair.
func (t *Tokenizer) bpe(token string) []string {
	if cached, ok := t.cache[token]; ok {
		return cached
	}
	runes := []rune(token)
	word := make([]string, len(runes))
	for i, r := range runes {
		word[i] = string(r)
	}
	word[len(word)-1] += "</w>"

	for len(word) > 1 {
		best, bestRank := -1, math.MaxInt
		for i := 0; i < len(word)-1; i++ {
			if rank, ok := t.bpeRanks[[2]string{word[i], word[i+1]}]; ok && rank < bestRank {
				best, bestRank = i, rank
			}
		}
		if best < 0 {
			break
		}
		first, second := word[best], word[best+1]
		merged := make([]string, 0, len(word)-1)
		for i := 0; i < len(word); i++ {
			if i < len(word)-1 && word[i] == first && word[i+1] == second {
				merged = append(merged, first+second)
				i++
			} else {
				merged = append(merged, word[i])
			}
		}
		word = merged
	}
	t.cache[token] = word
	return word
}

// bytesToUnicode maps every byte to a printable unicode character, as in GPT-2/CLIP.
func bytesToUnicode() [256]string {
	var table [256]string
	printable := func(b int) bool {
		return (b >= '!' && b <= '~') || (b >= 0xA1 && b <= 0xAC) || (b >= 0xAE && b <= 0xFF)
	}
	n := 0
	for b := 0; b < 256; b++ {
		if printable(b) {
			table[b] = string(rune(b))
		} else {
			table[b] = string(rune(256 + n))
			n++
		}
	}
	return table
}
