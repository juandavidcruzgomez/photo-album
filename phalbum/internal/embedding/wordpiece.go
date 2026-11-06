package embedding

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// WordPieceMaxLength is the sequence length the multilingual text encoder was
// trained with (sentence-transformers' max_seq_length).
const WordPieceMaxLength = 128

// maxWordChars mirrors BERT's max_input_chars_per_word: longer words become [UNK].
const maxWordChars = 100

// WordPiece is a pure Go implementation of BERT's cased WordPiece tokenizer
// (no lowercasing, no accent stripping), as used by multilingual DistilBERT.
type WordPiece struct {
	vocab map[string]int64
	unkID int64
	clsID int64
	sepID int64
	padID int64
}

// NewWordPiece loads a vocab.txt file with one token per line, the line
// number being the token id.
func NewWordPiece(vocabPath string) (*WordPiece, error) {
	f, err := os.Open(vocabPath)
	if err != nil {
		return nil, fmt.Errorf("reading vocab: %w", err)
	}
	defer f.Close()
	w := &WordPiece{vocab: make(map[string]int64)}
	scanner := bufio.NewScanner(f)
	var id int64
	for scanner.Scan() {
		w.vocab[strings.TrimRight(scanner.Text(), "\r")] = id
		id++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading vocab: %w", err)
	}
	for _, s := range []struct {
		token string
		id    *int64
	}{{"[UNK]", &w.unkID}, {"[CLS]", &w.clsID}, {"[SEP]", &w.sepID}, {"[PAD]", &w.padID}} {
		var ok bool
		if *s.id, ok = w.vocab[s.token]; !ok {
			return nil, fmt.Errorf("vocab is missing %s", s.token)
		}
	}
	return w, nil
}

// Encode returns the token ids for text wrapped in [CLS]/[SEP], truncated to
// WordPieceMaxLength. The result is not padded.
func (w *WordPiece) Encode(text string) []int64 {
	ids := []int64{w.clsID}
	for _, word := range basicTokenize(text) {
		ids = append(ids, w.wordPiece(word)...)
	}
	if len(ids) > WordPieceMaxLength-1 {
		ids = ids[:WordPieceMaxLength-1]
	}
	return append(ids, w.sepID)
}

// wordPiece splits a word into the longest vocabulary pieces, left to right.
// A word that cannot be fully covered becomes a single [UNK].
func (w *WordPiece) wordPiece(word string) []int64 {
	runes := []rune(word)
	if len(runes) > maxWordChars {
		return []int64{w.unkID}
	}
	var ids []int64
	for start := 0; start < len(runes); {
		end := len(runes)
		found := false
		for ; end > start; end-- {
			piece := string(runes[start:end])
			if start > 0 {
				piece = "##" + piece
			}
			if id, ok := w.vocab[piece]; ok {
				ids = append(ids, id)
				found = true
				break
			}
		}
		if !found {
			return []int64{w.unkID}
		}
		start = end
	}
	return ids
}

// basicTokenize cleans text and splits it on whitespace and punctuation, with
// every CJK character as its own token, like BERT's BasicTokenizer.
func basicTokenize(text string) []string {
	var b strings.Builder
	for _, r := range norm.NFC.String(text) {
		switch {
		case r == 0 || r == unicode.ReplacementChar:
		case isBertWhitespace(r):
			b.WriteRune(' ')
		case isBertControl(r):
		case isCJK(r) || isBertPunct(r):
			b.WriteRune(' ')
			b.WriteRune(r)
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Fields(b.String())
}

func isBertWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || unicode.Is(unicode.Zs, r)
}

func isBertControl(r rune) bool {
	return unicode.Is(unicode.C, r)
}

// isBertPunct treats all non-alphanumeric ASCII symbols as punctuation, plus
// the Unicode punctuation categories.
func isBertPunct(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}

// isCJK reports whether r is in the CJK Unified Ideographs blocks. Japanese
// kana and Hangul are not included, as in BERT.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		(r >= 0x2B820 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x2F800 && r <= 0x2FA1F)
}
