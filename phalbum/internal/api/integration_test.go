package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/dbtest"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
)

// TestWithDatabase runs the API on the real library: tag a photo through the
// API, then filter by that tag.
func TestWithDatabase(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	lib, err := library.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for i, name := range []string{"dog", "cat"} {
		id, err := lib.SavePhoto(ctx, library.Photo{
			ContentHash: name, ThumbnailKey: "t/" + name, Filename: name + ".jpg", MimeType: "image/jpeg",
			Extension: "jpg", Size: 1, Width: 1, Height: 1, Embedding: axis(i), Model: "test",
		}, []string{"Pets"})
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	f := &fixture{h: (&Server{Library: lib, Embedder: &fakeEmbedder{}, Objects: newObjects(map[string]string{}),
		Ping: pool.Ping, Log: log.New(io.Discard, "", 0)}).Handler()}

	if rec := f.do("GET", "/api/health", nil, ""); rec.Code != 200 {
		t.Fatalf("health: %d", rec.Code)
	}
	rec := f.do("POST", "/api/photos/"+ids["cat"]+"/tags", strings.NewReader(`{"name":"Favourite"}`), "application/json")
	if rec.Code != 200 {
		t.Fatalf("tag: %d %s", rec.Code, rec.Body)
	}

	list := func(query string) []photo {
		rec := f.do("GET", "/api/photos?"+query, nil, "")
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body)
		}
		var resp listResponse
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.Photos
	}
	if got := list("tags=favourite"); len(got) != 1 || got[0].Filename != "cat.jpg" || len(got[0].Tags) != 2 {
		t.Errorf("filter by new tag: %+v", got)
	}
	// The fake embedder maps any text to axis 0, i.e. the dog.
	if got := list("q=anything&tags=pets"); len(got) != 2 || got[0].Filename != "dog.jpg" || got[0].Score == nil {
		t.Errorf("text search: %+v", got)
	}
	if got := list("similar_to=" + ids["cat"] + "&limit=1"); len(got) != 1 || got[0].Filename != "cat.jpg" {
		t.Errorf("similar_to: %+v", got)
	}
	if rec := f.do("GET", "/api/photos/"+ids["dog"], nil, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"Pets"`) {
		t.Errorf("detail: %d %s", rec.Code, rec.Body)
	}
}
