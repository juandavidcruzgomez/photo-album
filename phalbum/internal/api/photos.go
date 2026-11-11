package api

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/imaging"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	maxUpload    = 20 << 20
)

// photo is a search result with the URLs the web app needs.
type photo struct {
	library.Result
	ThumbnailURL string `json:"thumbnail_url"`
	OriginalURL  string `json:"original_url"`
}

type listResponse struct {
	Photos     []photo `json:"photos"`
	NextOffset *int    `json:"next_offset"`
}

func withURLs(r library.Result) photo {
	base := "/api/photos/" + url.PathEscape(r.ID)
	return photo{Result: r, ThumbnailURL: base + "/thumbnail", OriginalURL: base + "/original"}
}

// listPhotos browses or searches with query parameters:
// q, similar_to, tags, from, to, sort, limit, offset.
func (s *Server) listPhotos(w http.ResponseWriter, r *http.Request) {
	s.search(w, r, nil)
}

// searchByImage searches with an uploaded picture (multipart field "image"),
// plus the same optional fields as listPhotos.
func (s *Server) searchByImage(w http.ResponseWriter, r *http.Request) {
	if s.NoImageSearch {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "searching by image is not available on this server"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<20)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		s.writeError(w, r, errBadRequest("expected a multipart form with an image under 20 MB"))
		return
	}
	f, _, err := r.FormFile("image")
	if err != nil {
		s.writeError(w, r, errBadRequest(`missing "image" file`))
		return
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		s.writeError(w, r, errBadRequest("reading image: "+err.Error()))
		return
	}
	prep, err := imaging.Prepare(data, imaging.EmbedSize)
	if errors.Is(err, image.ErrFormat) {
		s.writeError(w, r, errBadRequest("unsupported image format"))
		return
	}
	if err != nil {
		s.writeError(w, r, errBadRequest("decoding image: "+err.Error()))
		return
	}
	s.embedMu.Lock()
	embs, err := s.Embedder.EmbedImages(prep.Image)
	s.embedMu.Unlock()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.search(w, r, embs[0])
}

// search runs a query built from the request's form values, combining any
// given embedding with the text and similar_to ones.
func (s *Server) search(w http.ResponseWriter, r *http.Request, imageEmb []float32) {
	q, err := parseFilters(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var vectors [][]float32
	if imageEmb != nil {
		vectors = append(vectors, imageEmb)
	}
	if text := strings.TrimSpace(r.FormValue("q")); text != "" {
		s.embedMu.Lock()
		embs, err := s.Embedder.EmbedTexts(text)
		s.embedMu.Unlock()
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		vectors = append(vectors, embs[0])
	}
	if id := r.FormValue("similar_to"); id != "" {
		emb, err := s.Library.Embedding(r.Context(), id)
		if errors.Is(err, library.ErrNotFound) {
			s.writeError(w, r, errBadRequest("similar_to: unknown photo"))
			return
		}
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		vectors = append(vectors, emb)
	}
	q.Embedding = combine(vectors)

	results, more, err := s.Library.Search(r.Context(), q)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	resp := listResponse{Photos: make([]photo, len(results))}
	for i, res := range results {
		resp.Photos[i] = withURLs(res)
	}
	if more {
		next := q.Offset + q.Limit
		resp.NextOffset = &next
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseFilters reads tags, from, to, sort, limit and offset.
func parseFilters(r *http.Request) (library.Query, error) {
	q := library.Query{Limit: defaultLimit}
	if err := r.ParseForm(); err != nil {
		return q, errBadRequest("invalid query: " + err.Error())
	}
	for _, v := range r.Form["tags"] {
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				q.Tags = append(q.Tags, t)
			}
		}
	}
	if v := r.FormValue("from"); v != "" {
		t, err := parseDate(v)
		if err != nil {
			return q, errBadRequest("from: " + err.Error())
		}
		q.From = &t
	}
	if v := r.FormValue("to"); v != "" {
		t, err := parseDate(v)
		if err != nil {
			return q, errBadRequest("to: " + err.Error())
		}
		if len(v) == len(time.DateOnly) {
			t = t.AddDate(0, 0, 1) // a bare date includes the whole day
		} else {
			t = t.Add(time.Nanosecond)
		}
		q.Before = &t
	}
	switch r.FormValue("sort") {
	case "", "desc":
	case "asc":
		q.Ascending = true
	default:
		return q, errBadRequest(`sort: use "asc" or "desc"`)
	}
	var err error
	if q.Limit, err = intParam(r, "limit", defaultLimit); err != nil {
		return q, err
	}
	q.Limit = min(max(q.Limit, 1), maxLimit)
	if q.Offset, err = intParam(r, "offset", 0); err != nil {
		return q, err
	}
	q.Offset = max(q.Offset, 0)
	return q, nil
}

// parseDate accepts YYYY-MM-DD or RFC 3339.
func parseDate(v string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a date (use YYYY-MM-DD)", v)
}

func intParam(r *http.Request, name string, fallback int) (int, error) {
	v := r.FormValue(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, errBadRequest(name + ": not a number")
	}
	return n, nil
}

// combine averages unit vectors and renormalizes the result, so a query
// can mix text, an uploaded picture and a similar photo.
func combine(vectors [][]float32) []float32 {
	if len(vectors) == 0 {
		return nil
	}
	if len(vectors) == 1 {
		return vectors[0]
	}
	out := make([]float32, len(vectors[0]))
	for _, v := range vectors {
		for i, x := range v {
			out[i] += x
		}
	}
	var sum float64
	for _, x := range out {
		sum += float64(x) * float64(x)
	}
	if sum > 0 {
		inv := float32(1 / math.Sqrt(sum))
		for i := range out {
			out[i] *= inv
		}
	}
	return out
}

// listYears counts photos per capture year, with the same tag and date
// filters as listPhotos.
func (s *Server) listYears(w http.ResponseWriter, r *http.Request) {
	q, err := parseFilters(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	years, err := s.Library.Years(r.Context(), q)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"years": years})
}

func (s *Server) getPhoto(w http.ResponseWriter, r *http.Request) {
	d, err := s.Library.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	p := withURLs(d.Result)
	writeJSON(w, http.StatusOK, struct {
		*library.Detail
		ThumbnailURL string `json:"thumbnail_url"`
		OriginalURL  string `json:"original_url"`
	}{d, p.ThumbnailURL, p.OriginalURL})
}

// thumbnail serves the JPEG thumbnail from object storage. Keys are
// content-addressed, so it can be cached forever. When storage can't
// return it (missing, corrupted, too slow), it is rebuilt from the source
// file and stored again.
func (s *Server) thumbnail(w http.ResponseWriter, r *http.Request) {
	files, err := s.Library.Files(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	data, contentType, err := s.readThumbnail(r.Context(), files.ThumbnailKey)
	if err != nil {
		if r.Context().Err() != nil {
			return // the browser gave up on it
		}
		s.logger().Printf("thumbnail %s: %v; rebuilding from source", files.ThumbnailKey, err)
		data, contentType = nil, "image/jpeg"
		if data, err = s.rebuildThumbnail(r.Context(), files); err != nil {
			if r.Context().Err() == nil {
				s.logger().Printf("rebuilding %s: %v", files.ThumbnailKey, err)
			}
			s.writeError(w, r, sourceError(err))
			return
		}
	}
	if contentType == "" || contentType == "binary/octet-stream" {
		contentType = "image/jpeg"
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}

// original serves the full image: from object storage when it was copied
// there, otherwise (or when storage fails) from the first source path still
// present on disk.
func (s *Server) original(w http.ResponseWriter, r *http.Request) {
	files, err := s.Library.Files(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if files.OriginalKey != "" {
		obj, err := s.openOriginal(r.Context(), files.OriginalKey)
		if err == nil {
			defer obj.Body.Close()
			s.stream(w, obj, files.MimeType)
			if _, err := io.Copy(w, obj.Body); err != nil && r.Context().Err() == nil {
				s.logger().Printf("original %s: %v", files.OriginalKey, err)
				s.repairOriginal(files)
			}
			return
		}
		if r.Context().Err() != nil {
			return
		}
		s.logger().Printf("original %s: %v; serving the source file", files.OriginalKey, err)
		s.repairOriginal(files)
	}
	if !s.serveFromPaths(w, r, files) {
		s.writeError(w, r, library.ErrNotFound)
	}
}

// openOriginal starts reading an original, giving up if storage doesn't
// answer in time. The body can then take as long as it needs.
func (s *Server) openOriginal(ctx context.Context, key string) (*storage.Object, error) {
	ctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(originalTimeout, cancel)
	obj, err := s.Objects.Get(ctx, key)
	if err != nil || !timer.Stop() {
		cancel()
		if err == nil {
			obj.Body.Close()
			err = context.DeadlineExceeded
		}
		return nil, err
	}
	obj.Body = cancelOnClose{obj.Body, cancel}
	return obj, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// serveFromPaths serves the first source path that is a readable file.
func (s *Server) serveFromPaths(w http.ResponseWriter, r *http.Request, files *library.Files) bool {
	for _, path := range files.Paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		w.Header().Set("Content-Type", files.MimeType)
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return true
	}
	return false
}

// stream writes the headers for an object from storage.
func (s *Server) stream(w http.ResponseWriter, obj *storage.Object, contentType string) {
	if obj.ContentType != "" && obj.ContentType != "binary/octet-stream" {
		contentType = obj.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	if obj.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
}
