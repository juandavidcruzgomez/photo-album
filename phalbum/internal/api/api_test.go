package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

const photoID = "11111111-1111-1111-1111-111111111111"

type fakeLibrary struct {
	lastQuery library.Query
	files     library.Files
	tagged    []string
	renameErr error
}

func (f *fakeLibrary) Search(_ context.Context, q library.Query) ([]library.Result, bool, error) {
	f.lastQuery = q
	return []library.Result{{ID: photoID, Filename: "a.jpg", Tags: []library.TagRef{}}}, true, nil
}
func (f *fakeLibrary) Get(_ context.Context, id string) (*library.Detail, error) {
	if id != photoID {
		return nil, library.ErrNotFound
	}
	return &library.Detail{Result: library.Result{ID: id, Filename: "a.jpg"}, Paths: []string{"/x/a.jpg"}}, nil
}
func (f *fakeLibrary) Embedding(_ context.Context, id string) ([]float32, error) {
	if id != photoID {
		return nil, library.ErrNotFound
	}
	return axis(1), nil
}
func (f *fakeLibrary) Files(_ context.Context, id string) (*library.Files, error) {
	if id != photoID {
		return nil, library.ErrNotFound
	}
	return &f.files, nil
}
func (f *fakeLibrary) ListTags(context.Context) ([]library.Tag, error) {
	return []library.Tag{{ID: 1, Name: "Paris", Photos: 2}}, nil
}
func (f *fakeLibrary) CreateTag(_ context.Context, name string) (library.Tag, error) {
	if strings.TrimSpace(name) == "" {
		return library.Tag{}, library.ErrEmptyTag
	}
	return library.Tag{ID: 2, Name: name}, nil
}
func (f *fakeLibrary) RenameTag(_ context.Context, id int64, name string) (library.Tag, error) {
	return library.Tag{ID: id, Name: name}, f.renameErr
}
func (f *fakeLibrary) DeleteTag(_ context.Context, id int64) error {
	if id != 1 {
		return library.ErrNotFound
	}
	return nil
}
func (f *fakeLibrary) TagPhoto(_ context.Context, id, name string) (library.TagRef, error) {
	f.tagged = append(f.tagged, id+":"+name)
	return library.TagRef{ID: 3, Name: name, Source: library.SourceUser}, nil
}
func (f *fakeLibrary) UntagPhoto(_ context.Context, id string, tagID int64) error { return nil }
func (f *fakeLibrary) TagPhotos(_ context.Context, ids, add []string, remove []int64) error {
	f.tagged = append(f.tagged, strings.Join(ids, ",")+"+"+strings.Join(add, ","))
	return nil
}

func axis(i int) []float32 {
	v := make([]float32, 512)
	v[i] = 1
	return v
}

type fakeEmbedder struct{ texts, images int }

func (e *fakeEmbedder) EmbedTexts(texts ...string) ([][]float32, error) {
	e.texts++
	return [][]float32{axis(0)}, nil
}
func (e *fakeEmbedder) EmbedImages(imgs ...image.Image) ([][]float32, error) {
	e.images++
	return [][]float32{axis(2)}, nil
}

type fakeObjects map[string]string

func (o fakeObjects) Get(_ context.Context, key string) (*storage.Object, error) {
	body, ok := o[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &storage.Object{Body: io.NopCloser(strings.NewReader(body)), Size: int64(len(body))}, nil
}

type fixture struct {
	lib *fakeLibrary
	emb *fakeEmbedder
	h   http.Handler
}

func newFixture() *fixture {
	lib := &fakeLibrary{files: library.Files{ThumbnailKey: "thumbnails/a.jpg", MimeType: "image/jpeg"}}
	emb := &fakeEmbedder{}
	s := &Server{
		Library: lib, Embedder: emb, Objects: fakeObjects{"thumbnails/a.jpg": "JPEG", "originals/a.jpg": "ORIG"},
		CORSOrigins: []string{"http://app.local"}, Log: log.New(io.Discard, "", 0),
	}
	return &fixture{lib: lib, emb: emb, h: s.Handler()}
}

func (f *fixture) do(method, target string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func TestListPhotosParsing(t *testing.T) {
	f := newFixture()
	rec := f.do("GET", "/api/photos?tags=Paris,%202024&tags=beach&from=2024-01-01&to=2024-12-31&limit=500&offset=-3", nil, "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	q := f.lib.lastQuery
	if strings.Join(q.Tags, "|") != "Paris|2024|beach" || q.Limit != maxLimit || q.Offset != 0 || q.Embedding != nil {
		t.Errorf("query = %+v", q)
	}
	if q.From.Format("2006-01-02") != "2024-01-01" || q.Before.Format("2006-01-02") != "2025-01-01" {
		t.Errorf("dates: from=%v before=%v", q.From, q.Before)
	}

	var resp listResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Photos) != 1 || resp.Photos[0].ThumbnailURL != "/api/photos/"+photoID+"/thumbnail" ||
		resp.NextOffset == nil || *resp.NextOffset != maxLimit {
		t.Errorf("response = %s", rec.Body)
	}

	for _, bad := range []string{"from=yesterday", "to=2024-13-01", "limit=ten", "similar_to=nope"} {
		if rec := f.do("GET", "/api/photos?"+bad, nil, ""); rec.Code != 400 {
			t.Errorf("%s: status %d", bad, rec.Code)
		}
	}
}

func TestTextAndSimilarCombined(t *testing.T) {
	f := newFixture()
	rec := f.do("GET", "/api/photos?q=a+dog&similar_to="+photoID, nil, "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	e := f.lib.lastQuery.Embedding
	if f.emb.texts != 1 || e == nil || e[0] < 0.7 || e[0] > 0.71 || e[1] < 0.7 {
		t.Errorf("combined embedding starts %v (texts=%d)", e[:2], f.emb.texts)
	}
}

func TestSearchByImage(t *testing.T) {
	f := newFixture()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("tags", "Paris")
	fw, _ := mw.CreateFormFile("image", "query.png")
	png.Encode(fw, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	mw.Close()

	rec := f.do("POST", "/api/photos/search", &body, mw.FormDataContentType())
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if f.emb.images != 1 || f.lib.lastQuery.Embedding[2] != 1 || f.lib.lastQuery.Tags[0] != "Paris" {
		t.Errorf("images=%d query=%+v", f.emb.images, f.lib.lastQuery.Tags)
	}

	body.Reset()
	mw = multipart.NewWriter(&body)
	fw, _ = mw.CreateFormFile("image", "notes.txt")
	fw.Write([]byte("hello"))
	mw.Close()
	if rec := f.do("POST", "/api/photos/search", &body, mw.FormDataContentType()); rec.Code != 400 {
		t.Errorf("non-image upload: status %d", rec.Code)
	}
	if rec := f.do("POST", "/api/photos/search", strings.NewReader("{}"), "application/json"); rec.Code != 400 {
		t.Errorf("non-multipart: status %d", rec.Code)
	}
}

func TestPhotoDetailAndFiles(t *testing.T) {
	f := newFixture()
	if rec := f.do("GET", "/api/photos/"+photoID, nil, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"original_url"`) {
		t.Errorf("detail: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do("GET", "/api/photos/nope", nil, ""); rec.Code != 404 {
		t.Errorf("missing photo: %d", rec.Code)
	}

	rec := f.do("GET", "/api/photos/"+photoID+"/thumbnail", nil, "")
	if rec.Code != 200 || rec.Body.String() != "JPEG" || rec.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("thumbnail: %d %q %v", rec.Code, rec.Body, rec.Header())
	}

	// No stored original: served from the first source path that exists.
	dir := t.TempDir()
	local := filepath.Join(dir, "a.jpg")
	os.WriteFile(local, []byte("LOCAL"), 0o644)
	f.lib.files.Paths = []string{filepath.Join(dir, "gone.jpg"), local}
	if rec := f.do("GET", "/api/photos/"+photoID+"/original", nil, ""); rec.Code != 200 || rec.Body.String() != "LOCAL" {
		t.Errorf("local original: %d %q", rec.Code, rec.Body)
	}
	f.lib.files.OriginalKey = "originals/a.jpg"
	if rec := f.do("GET", "/api/photos/"+photoID+"/original", nil, ""); rec.Body.String() != "ORIG" {
		t.Errorf("stored original: %q", rec.Body)
	}
	f.lib.files.OriginalKey = ""
	f.lib.files.Paths = []string{filepath.Join(dir, "gone.jpg")}
	if rec := f.do("GET", "/api/photos/"+photoID+"/original", nil, ""); rec.Code != 404 {
		t.Errorf("no original anywhere: %d", rec.Code)
	}
}

func TestTagEndpoints(t *testing.T) {
	f := newFixture()
	if rec := f.do("GET", "/api/tags", nil, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"Paris"`) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do("POST", "/api/tags", strings.NewReader(`{"name":"Beach"}`), "application/json"); rec.Code != 200 {
		t.Errorf("create: %d", rec.Code)
	}
	if rec := f.do("POST", "/api/tags", strings.NewReader(`{"name":" "}`), "application/json"); rec.Code != 400 {
		t.Errorf("create empty: %d", rec.Code)
	}
	if rec := f.do("POST", "/api/tags", strings.NewReader(`{bad`), "application/json"); rec.Code != 400 {
		t.Errorf("bad json: %d", rec.Code)
	}
	f.lib.renameErr = library.ErrTagExists
	if rec := f.do("PATCH", "/api/tags/1", strings.NewReader(`{"name":"Paris"}`), "application/json"); rec.Code != 409 {
		t.Errorf("rename conflict: %d", rec.Code)
	}
	if rec := f.do("PATCH", "/api/tags/abc", strings.NewReader(`{"name":"x"}`), "application/json"); rec.Code != 400 {
		t.Errorf("bad tag id: %d", rec.Code)
	}
	if rec := f.do("DELETE", "/api/tags/1", nil, ""); rec.Code != 204 {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := f.do("DELETE", "/api/tags/9", nil, ""); rec.Code != 404 {
		t.Errorf("delete missing: %d", rec.Code)
	}
	if rec := f.do("POST", "/api/photos/"+photoID+"/tags", strings.NewReader(`{"name":"Trip"}`), "application/json"); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"source":"user"`) {
		t.Errorf("tag photo: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do("DELETE", "/api/photos/"+photoID+"/tags/3", nil, ""); rec.Code != 204 {
		t.Errorf("untag: %d", rec.Code)
	}
	if rec := f.do("POST", "/api/photos/tags", strings.NewReader(`{"photo_ids":["p1","p2"],"add":["X"]}`), "application/json"); rec.Code != 204 {
		t.Errorf("bulk: %d", rec.Code)
	}
	if rec := f.do("POST", "/api/photos/tags", strings.NewReader(`{"add":["X"]}`), "application/json"); rec.Code != 400 {
		t.Errorf("bulk without photos: %d", rec.Code)
	}
	if got := strings.Join(f.lib.tagged, " "); got != photoID+":Trip p1,p2+X" {
		t.Errorf("tagged = %s", got)
	}
}

func TestCORS(t *testing.T) {
	f := newFixture()
	req := httptest.NewRequest("OPTIONS", "/api/tags", nil)
	req.Header.Set("Origin", "http://app.local")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "http://app.local" ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Errorf("preflight: %d %v", rec.Code, rec.Header())
	}

	req = httptest.NewRequest("GET", "/api/tags", nil)
	req.Header.Set("Origin", "http://evil.local")
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("unknown origin must not be allowed")
	}
}
