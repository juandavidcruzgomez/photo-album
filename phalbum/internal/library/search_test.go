package library

import (
	"context"
	"errors"
	"testing"
	"time"
)

func date(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

// seed saves photos a, b, c with embeddings along axes 0, 1, 2. a and b
// have EXIF dates; c only has a file date from crawled_files.
func seed(t *testing.T, s *Store) map[string]string {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO crawled_files (path, size, mod_time, content_hash)
		VALUES ('/pics/c.jpg', 1, '2023-05-05', 'c'), ('/backup/c.jpg', 1, '2023-07-07', 'c')`); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for i, h := range []string{"a", "b", "c"} {
		p := photo(h, unit(i))
		switch h {
		case "a":
			p.TakenAt = date("2024-06-01")
		case "b":
			p.TakenAt = date("2022-01-15")
		}
		tags := map[string][]string{"a": {"Paris", "2024"}, "b": {"paris"}, "c": {"Home"}}[h]
		id, err := s.SavePhoto(ctx, p, tags)
		if err != nil {
			t.Fatal(err)
		}
		ids[h] = id
	}
	return ids
}

func names(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Filename[:1])
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearch(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s)

	near := unit(2)
	near[1] = 0.5
	cases := []struct {
		name string
		q    Query
		want []string
		more bool
	}{
		{"browse newest first, file date fallback", Query{Limit: 10}, []string{"a", "c", "b"}, false},
		{"paging", Query{Limit: 2}, []string{"a", "c"}, true},
		{"second page", Query{Limit: 2, Offset: 2}, []string{"b"}, false},
		{"similarity", Query{Embedding: near, Limit: 10}, []string{"c", "b", "a"}, false},
		{"tag, case-insensitive", Query{Tags: []string{"PARIS"}, Limit: 10}, []string{"a", "b"}, false},
		{"all tags required", Query{Tags: []string{"paris", "2024"}, Limit: 10}, []string{"a"}, false},
		{"unknown tag", Query{Tags: []string{"nope"}, Limit: 10}, nil, false},
		{"date range", Query{From: date("2023-01-01"), Before: date("2024-01-01"), Limit: 10}, []string{"c"}, false},
		{"similarity + filter", Query{Embedding: near, Tags: []string{"paris"}, Limit: 10}, []string{"b", "a"}, false},
	}
	for _, c := range cases {
		got, more, err := s.Search(ctx, c.q)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !eq(names(got), c.want) || more != c.more {
			t.Errorf("%s: got %v more=%v, want %v more=%v", c.name, names(got), more, c.want, c.more)
		}
	}

	got, _, _ := s.Search(ctx, Query{Embedding: unit(0), Limit: 1})
	if got[0].Score == nil || *got[0].Score < 0.99 {
		t.Errorf("score = %v", got[0].Score)
	}
	got, _, _ = s.Search(ctx, Query{Tags: []string{"2024"}, Limit: 1})
	if len(got[0].Tags) != 2 || got[0].Tags[0].Name != "2024" || got[0].Score != nil {
		t.Errorf("tags/score on browse result: %+v", got[0])
	}
}

func TestGetAndFiles(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ids := seed(t, s)

	d, err := s.Get(ctx, ids["c"])
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Paths) != 2 || d.CapturedAt == nil || d.CapturedAt.Year() != 2023 || d.TakenAt != nil ||
		len(d.Tags) != 1 || d.Exif["Model"] != "X100" || d.HasOriginal {
		t.Errorf("detail: %+v", d)
	}

	emb, err := s.Embedding(ctx, ids["b"])
	if err != nil || len(emb) != 512 || emb[1] != 1 {
		t.Errorf("embedding: len=%d err=%v", len(emb), err)
	}
	f, err := s.Files(ctx, ids["c"])
	if err != nil || f.ThumbnailKey != "thumbnails/c.jpg" || f.Paths[0] != "/backup/c.jpg" {
		t.Errorf("files: %+v err=%v", f, err)
	}

	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%s): %v", id, err)
		}
		if _, err := s.Embedding(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Embedding(%s): %v", id, err)
		}
	}
}

func TestTagManagement(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ids := seed(t, s)

	tag, err := s.CreateTag(ctx, " Beach ")
	if err != nil || tag.Name != "Beach" || tag.Photos != 0 {
		t.Fatalf("create: %+v %v", tag, err)
	}
	if again, _ := s.CreateTag(ctx, "beach"); again.ID != tag.ID {
		t.Error("creating an existing tag should return it")
	}
	if _, err := s.CreateTag(ctx, "  "); !errors.Is(err, ErrEmptyTag) {
		t.Errorf("empty tag: %v", err)
	}

	ref, err := s.TagPhoto(ctx, ids["a"], "beach")
	if err != nil || ref.ID != tag.ID || ref.Source != SourceUser {
		t.Fatalf("tag photo: %+v %v", ref, err)
	}
	if _, err := s.TagPhoto(ctx, "00000000-0000-0000-0000-000000000000", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("tag missing photo: %v", err)
	}

	if _, err := s.RenameTag(ctx, tag.ID, "PARIS"); !errors.Is(err, ErrTagExists) {
		t.Errorf("rename conflict: %v", err)
	}
	if r, err := s.RenameTag(ctx, tag.ID, "BEACH"); err != nil || r.Name != "BEACH" || r.Photos != 1 {
		t.Errorf("rename case: %+v %v", r, err)
	}
	if _, err := s.RenameTag(ctx, 99999, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename missing: %v", err)
	}

	if err := s.UntagPhoto(ctx, ids["a"], tag.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UntagPhoto(ctx, ids["a"], tag.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("untag twice: %v", err)
	}

	// Bulk: add "Trip" to a and b, remove Paris from both.
	tags, _ := s.ListTags(ctx)
	var paris int64
	for _, t := range tags {
		if t.Name == "Paris" {
			paris = t.ID
		}
	}
	if err := s.TagPhotos(ctx, []string{ids["a"], ids["b"]}, []string{"Trip"}, []int64{paris}); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.Search(ctx, Query{Tags: []string{"trip"}, Limit: 10})
	if len(got) != 2 {
		t.Errorf("trip photos: %v", names(got))
	}
	if got, _, _ := s.Search(ctx, Query{Tags: []string{"paris"}, Limit: 10}); len(got) != 0 {
		t.Errorf("paris should be gone: %v", names(got))
	}
	if err := s.TagPhotos(ctx, []string{ids["a"], "00000000-0000-0000-0000-000000000000"}, []string{"X"}, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("bulk with missing photo: %v", err)
	}
	if got, _, _ := s.Search(ctx, Query{Tags: []string{"x"}, Limit: 10}); len(got) != 0 {
		t.Error("failed bulk call must not change anything")
	}

	if err := s.DeleteTag(ctx, tag.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTag(ctx, tag.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete twice: %v", err)
	}
	tags, _ = s.ListTags(ctx)
	for _, tg := range tags {
		if tg.Name == "Trip" && tg.Photos != 2 {
			t.Errorf("Trip count = %d", tg.Photos)
		}
	}
}
