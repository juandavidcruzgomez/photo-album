// Package library stores indexed photos: metadata, CLIP embeddings and tags.
package library

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/db"
)

//go:embed schema.sql
var schema string

// Tag sources.
const (
	SourcePath = "path" // folder names, from the crawler
	SourceUser = "user" // added by hand in the web app
	SourceAuto = "auto" // suggested by a model
)

// Store gives access to the photo tables.
type Store struct {
	pool *pgxpool.Pool
}

// New makes sure the schema exists. The pool stays owned by the caller.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := db.ApplySchema(ctx, pool, schema); err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Photo is an indexed image.
type Photo struct {
	ContentHash  string
	OriginalKey  string // empty when the original isn't stored
	ThumbnailKey string
	Filename     string
	MimeType     string
	Extension    string
	Size         int64
	Width        int
	Height       int
	TakenAt      *time.Time
	CameraMake   string
	CameraModel  string
	Latitude     *float64
	Longitude    *float64
	Exif         map[string]string
	Embedding    []float32
	Model        string
}

// SavePhoto inserts or updates a photo (keyed by content hash) and links
// the given folder tags to it, in one transaction. Re-saving a photo is
// harmless, and existing tag links (e.g. added by a user) are kept.
func (s *Store) SavePhoto(ctx context.Context, p Photo, pathTags []string) (id string, err error) {
	var exif []byte
	if p.Exif != nil {
		if exif, err = json.Marshal(p.Exif); err != nil {
			return "", err
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO photos (content_hash, original_key, thumbnail_key, filename, mime_type, extension,
			                    size, width, height, taken_at, camera_make, camera_model,
			                    latitude, longitude, exif, embedding, model)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::vector, $17)
			ON CONFLICT (content_hash) DO UPDATE SET
			    original_key = COALESCE(EXCLUDED.original_key, photos.original_key),
			    thumbnail_key = EXCLUDED.thumbnail_key, filename = EXCLUDED.filename,
			    mime_type = EXCLUDED.mime_type, extension = EXCLUDED.extension, size = EXCLUDED.size,
			    width = EXCLUDED.width, height = EXCLUDED.height, taken_at = EXCLUDED.taken_at,
			    camera_make = EXCLUDED.camera_make, camera_model = EXCLUDED.camera_model,
			    latitude = EXCLUDED.latitude, longitude = EXCLUDED.longitude, exif = EXCLUDED.exif,
			    embedding = EXCLUDED.embedding, model = EXCLUDED.model, indexed_at = now()
			RETURNING id::text`,
			p.ContentHash, nullable(p.OriginalKey), p.ThumbnailKey, p.Filename, p.MimeType, p.Extension,
			p.Size, p.Width, p.Height, p.TakenAt, nullable(p.CameraMake), nullable(p.CameraModel),
			p.Latitude, p.Longitude, exif, VectorLiteral(p.Embedding), p.Model,
		).Scan(&id)
		if err != nil {
			return err
		}
		for _, name := range pathTags {
			if err := addTag(ctx, tx, id, name, SourcePath); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("saving photo %s: %w", p.ContentHash, err)
	}
	return id, nil
}

// AddTag links a tag (created if needed, matched case-insensitively) to a
// photo. An existing link is left untouched.
func (s *Store) AddTag(ctx context.Context, photoID, name, source string) error {
	return addTag(ctx, s.pool, photoID, name, source)
}

// querier is satisfied by both the pool and a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func addTag(ctx context.Context, q querier, photoID, name, source string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	_, err := q.Exec(ctx, `
		WITH t AS (
			INSERT INTO tags (name) VALUES ($2)
			ON CONFLICT (lower(name)) DO UPDATE SET name = tags.name
			RETURNING id
		)
		INSERT INTO photo_tags (photo_id, tag_id, source)
		SELECT $1, id, $3 FROM t
		ON CONFLICT (photo_id, tag_id) DO NOTHING`,
		photoID, name, source)
	return err
}

// VectorLiteral formats an embedding as a pgvector text literal, for use
// with a $n::vector parameter.
func VectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 12)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
