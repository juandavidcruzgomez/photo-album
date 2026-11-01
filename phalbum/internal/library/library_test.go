package library

import (
	"context"
	"testing"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/dbtest"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := New(context.Background(), dbtest.Pool(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// unit returns a 512-d unit vector along axis i.
func unit(i int) []float32 {
	v := make([]float32, 512)
	v[i] = 1
	return v
}

func photo(hash string, emb []float32) Photo {
	return Photo{
		ContentHash: hash, ThumbnailKey: "thumbnails/" + hash + ".jpg", Filename: hash + ".jpg",
		MimeType: "image/jpeg", Extension: "jpg", Size: 10, Width: 4, Height: 3,
		Exif: map[string]string{"Model": "X100"}, Embedding: emb, Model: "test",
	}
}

func TestSavePhoto(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	id, err := s.SavePhoto(ctx, photo("h1", unit(0)), []string{"2024", "Paris"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTag(ctx, id, "favourite", SourceUser); err != nil {
		t.Fatal(err)
	}

	// Re-indexing the same content: same row, tags deduplicated by case,
	// the user's tag kept, and a stored original isn't forgotten.
	p := photo("h1", unit(0))
	p.OriginalKey = "originals/h1.jpg"
	if _, err := s.SavePhoto(ctx, p, []string{"paris"}); err != nil {
		t.Fatal(err)
	}
	id2, err := s.SavePhoto(ctx, photo("h1", unit(0)), []string{"PARIS", "2024"})
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id {
		t.Errorf("re-save gave a new id: %s vs %s", id2, id)
	}

	var photos, tags, links int
	var original *string
	s.pool.QueryRow(ctx, "SELECT count(*), max(original_key) FROM photos").Scan(&photos, &original)
	s.pool.QueryRow(ctx, "SELECT count(*) FROM tags").Scan(&tags)
	s.pool.QueryRow(ctx, "SELECT count(*) FROM photo_tags").Scan(&links)
	if photos != 1 || tags != 3 || links != 3 {
		t.Errorf("photos=%d tags=%d links=%d, want 1/3/3", photos, tags, links)
	}
	if original == nil || *original != "originals/h1.jpg" {
		t.Errorf("original_key = %v", original)
	}
	var source string
	s.pool.QueryRow(ctx, `SELECT source FROM photo_tags JOIN tags ON tags.id = tag_id WHERE name = 'favourite'`).Scan(&source)
	if source != SourceUser {
		t.Errorf("user tag source = %q", source)
	}
}

func TestNearestNeighbour(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for i, h := range []string{"a", "b", "c"} {
		if _, err := s.SavePhoto(ctx, photo(h, unit(i)), nil); err != nil {
			t.Fatal(err)
		}
	}
	q := unit(1)
	q[0] = 0.3
	var hash string
	err := s.pool.QueryRow(ctx, "SELECT content_hash FROM photos ORDER BY embedding <=> $1::vector LIMIT 1",
		VectorLiteral(q)).Scan(&hash)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "b" {
		t.Errorf("nearest = %s, want b", hash)
	}
}

func TestVectorLiteral(t *testing.T) {
	if got := VectorLiteral([]float32{1, -0.5, 0.25}); got != "[1,-0.5,0.25]" {
		t.Errorf("VectorLiteral = %s", got)
	}
}
