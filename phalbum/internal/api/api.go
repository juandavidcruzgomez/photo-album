// Package api is the HTTP API used by the web app: photo search (by text,
// by picture, by similarity), filtering by tags and dates, tag management,
// and serving thumbnails and originals.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"log"
	"net/http"
	"sync"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/library"
	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

// Library is the part of *library.Store the API uses.
type Library interface {
	Search(ctx context.Context, q library.Query) ([]library.Result, bool, error)
	Get(ctx context.Context, id string) (*library.Detail, error)
	Embedding(ctx context.Context, id string) ([]float32, error)
	Files(ctx context.Context, id string) (*library.Files, error)
	ListTags(ctx context.Context) ([]library.Tag, error)
	CreateTag(ctx context.Context, name string) (library.Tag, error)
	RenameTag(ctx context.Context, id int64, name string) (library.Tag, error)
	DeleteTag(ctx context.Context, id int64) error
	TagPhoto(ctx context.Context, photoID, name string) (library.TagRef, error)
	UntagPhoto(ctx context.Context, photoID string, tagID int64) error
	TagPhotos(ctx context.Context, photoIDs, add []string, remove []int64) error
	Years(ctx context.Context, q library.Query) ([]library.YearCount, error)
}

// Embedder computes CLIP embeddings; *embedding.CLIP satisfies it.
type Embedder interface {
	EmbedTexts(texts ...string) ([][]float32, error)
	EmbedImages(imgs ...image.Image) ([][]float32, error)
}

// Objects reads from object storage; *storage.S3 satisfies it.
type Objects interface {
	Get(ctx context.Context, key string) (*storage.Object, error)
}

// Server serves the API.
type Server struct {
	Library     Library
	Embedder    Embedder
	Objects     Objects
	Static      http.Handler                // serves the web app for non-API paths; nil serves nothing
	Ping        func(context.Context) error // health check; nil means always healthy
	CORSOrigins []string                    // allowed origins; "*" allows any
	Log         *log.Logger                 // request and error log; nil uses the default logger
	// NoImageSearch makes searching by an uploaded picture answer 503, for
	// servers running without the vision model.
	NoImageSearch bool

	embedMu sync.Mutex // CLIP calls are serialized
}

// Handler returns the API's HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/photos", s.listPhotos)
	mux.HandleFunc("POST /api/photos/search", s.searchByImage)
	mux.HandleFunc("POST /api/photos/tags", s.tagPhotos)
	mux.HandleFunc("GET /api/years", s.listYears)
	mux.HandleFunc("GET /api/photos/{id}", s.getPhoto)
	mux.HandleFunc("GET /api/photos/{id}/thumbnail", s.thumbnail)
	mux.HandleFunc("GET /api/photos/{id}/original", s.original)
	mux.HandleFunc("POST /api/photos/{id}/tags", s.tagPhoto)
	mux.HandleFunc("DELETE /api/photos/{id}/tags/{tagID}", s.untagPhoto)
	mux.HandleFunc("GET /api/tags", s.listTags)
	mux.HandleFunc("POST /api/tags", s.createTag)
	mux.HandleFunc("PATCH /api/tags/{id}", s.renameTag)
	mux.HandleFunc("DELETE /api/tags/{id}", s.deleteTag)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	if s.Static != nil {
		mux.Handle("/", s.Static)
	}
	return s.logRequests(s.recoverPanics(s.cors(mux)))
}

func (s *Server) logger() *log.Logger {
	if s.Log != nil {
		return s.Log
	}
	return log.Default()
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if s.Ping != nil {
		if err := s.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// badRequest marks client errors.
type badRequest struct{ msg string }

func (e badRequest) Error() string { return e.msg }

func errBadRequest(msg string) error { return badRequest{msg} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError maps an error to a status code and a JSON body.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var bad badRequest
	status := http.StatusInternalServerError
	msg := "internal error"
	switch {
	case errors.As(err, &bad), errors.Is(err, library.ErrEmptyTag):
		status, msg = http.StatusBadRequest, err.Error()
	case errors.Is(err, library.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		status, msg = http.StatusNotFound, "not found"
	case errors.Is(err, library.ErrTagExists):
		status, msg = http.StatusConflict, err.Error()
	default:
		s.logger().Printf("error: %s %s: %v", r.Method, r.URL.Path, err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
