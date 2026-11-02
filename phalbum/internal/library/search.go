package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned for unknown photos and tags.
var ErrNotFound = errors.New("not found")

// Query selects photos. With an Embedding, results are ranked by similarity;
// without one they're listed newest first.
type Query struct {
	Embedding []float32
	Tags      []string   // photos must have all of these (case-insensitive)
	From      *time.Time // captured_at >= From
	Before    *time.Time // captured_at < Before
	Ascending bool       // oldest first when listing; ignored when ranking by similarity
	Limit     int
	Offset    int
}

// TagRef is a tag as attached to a photo.
type TagRef struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Result is a photo in a list or search result.
type Result struct {
	ID          string     `json:"id"`
	Filename    string     `json:"filename"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	CapturedAt  *time.Time `json:"captured_at"`
	TakenAt     *time.Time `json:"taken_at"`
	CameraModel string     `json:"camera_model,omitempty"`
	Tags        []TagRef   `json:"tags"`
	Score       *float64   `json:"score,omitempty"` // similarity in [-1, 1], only when searching
}

// Detail is everything known about one photo.
type Detail struct {
	Result
	MimeType    string            `json:"mime_type"`
	Extension   string            `json:"extension"`
	Size        int64             `json:"size"`
	CameraMake  string            `json:"camera_make,omitempty"`
	Latitude    *float64          `json:"latitude,omitempty"`
	Longitude   *float64          `json:"longitude,omitempty"`
	Exif        map[string]string `json:"exif,omitempty"`
	Paths       []string          `json:"paths"` // where the crawler found this content
	HasOriginal bool              `json:"has_original"`
	IndexedAt   time.Time         `json:"indexed_at"`
}

const resultColumns = `p.id::text, p.filename, p.width, p.height, p.captured_at, p.taken_at, COALESCE(p.camera_model, '')`

// Search returns one page of photos matching q, and whether more follow.
func (s *Store) Search(ctx context.Context, q Query) (results []Result, more bool, err error) {
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	cond := whereClause(q, arg)
	limit, offset := q.Limit+1, q.Offset // one extra row tells whether there's a next page

	var sql string
	if q.Embedding != nil {
		// The HNSW scan returns rows approximately ordered; re-sort the
		// candidates exactly before paging.
		v := arg(VectorLiteral(q.Embedding))
		sql = fmt.Sprintf(`
			WITH c AS MATERIALIZED (
				SELECT %s, p.embedding <=> %s::vector AS distance
				FROM photos p %s
				ORDER BY distance LIMIT %d
			)
			SELECT * FROM c ORDER BY distance LIMIT %d OFFSET %d`,
			resultColumns, v, cond, limit+offset, limit, offset)
	} else {
		dir := "DESC"
		if q.Ascending {
			dir = "ASC"
		}
		sql = fmt.Sprintf(`SELECT %s, NULL::float8 FROM photos p %s
			ORDER BY p.captured_at %s NULLS LAST, p.id LIMIT %d OFFSET %d`,
			resultColumns, cond, dir, limit, offset)
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Keep scanning the vector index until enough rows pass the filters.
		if _, err := tx.Exec(ctx, "SET LOCAL hnsw.iterative_scan = relaxed_order"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		results, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Result, error) {
			var r Result
			var distance *float64
			err := row.Scan(&r.ID, &r.Filename, &r.Width, &r.Height, &r.CapturedAt, &r.TakenAt, &r.CameraModel, &distance)
			if distance != nil {
				score := 1 - *distance
				r.Score = &score
			}
			return r, err
		})
		if err != nil {
			return err
		}
		return attachTags(ctx, tx, results)
	})
	if err != nil {
		return nil, false, err
	}
	if len(results) > q.Limit {
		results, more = results[:q.Limit], true
	}
	return results, more, nil
}

// whereClause builds the WHERE clause for q's tag and date filters, adding
// its parameters with arg. It returns "" when there are no filters.
func whereClause(q Query, arg func(any) string) string {
	var where []string
	if tags := normalizeTags(q.Tags); len(tags) > 0 {
		where = append(where, fmt.Sprintf(`p.id IN (
			SELECT pt.photo_id FROM photo_tags pt JOIN tags t ON t.id = pt.tag_id
			WHERE lower(t.name) = ANY(%s)
			GROUP BY pt.photo_id HAVING count(*) = %s)`, arg(tags), arg(len(tags))))
	}
	if q.From != nil {
		where = append(where, "p.captured_at >= "+arg(*q.From))
	}
	if q.Before != nil {
		where = append(where, "p.captured_at < "+arg(*q.Before))
	}
	if len(where) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(where, " AND ")
}

// YearCount is the number of photos captured in a year.
type YearCount struct {
	Year   int `json:"year"`
	Photos int `json:"photos"`
}

// Years counts the photos matching q's filters per capture year, newest
// year first. Photos without a date are left out.
func (s *Store) Years(ctx context.Context, q Query) ([]YearCount, error) {
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	cond := whereClause(q, arg)
	if cond == "" {
		cond = "WHERE p.captured_at IS NOT NULL"
	} else {
		cond += " AND p.captured_at IS NOT NULL"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT extract(year FROM p.captured_at)::int AS year, count(*)::int
		FROM photos p `+cond+` GROUP BY 1 ORDER BY 1 DESC`, args...)
	if err != nil {
		return nil, err
	}
	years, err := pgx.CollectRows(rows, pgx.RowToStructByPos[YearCount])
	if err != nil {
		return nil, err
	}
	if years == nil {
		years = []YearCount{}
	}
	return years, nil
}

// attachTags fills in the tags of each result with a single query.
func attachTags(ctx context.Context, tx pgx.Tx, results []Result) error {
	if len(results) == 0 {
		return nil
	}
	ids := make([]string, len(results))
	index := map[string]int{}
	for i, r := range results {
		ids[i] = r.ID
		index[r.ID] = i
		results[i].Tags = []TagRef{}
	}
	rows, err := tx.Query(ctx, `
		SELECT pt.photo_id::text, t.id, t.name, pt.source
		FROM photo_tags pt JOIN tags t ON t.id = pt.tag_id
		WHERE pt.photo_id = ANY($1::uuid[]) ORDER BY lower(t.name)`, ids)
	if err != nil {
		return err
	}
	var id string
	var t TagRef
	_, err = pgx.ForEachRow(rows, []any{&id, &t.ID, &t.Name, &t.Source}, func() error {
		i := index[id]
		results[i].Tags = append(results[i].Tags, t)
		return nil
	})
	return err
}

// Get returns one photo with all its details.
func (s *Store) Get(ctx context.Context, id string) (*Detail, error) {
	var d Detail
	var exif []byte
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT `+resultColumns+`, p.mime_type, p.extension, p.size, COALESCE(p.camera_make, ''),
			       p.latitude, p.longitude, p.exif, p.original_key IS NOT NULL, p.indexed_at,
			       ARRAY(SELECT path FROM crawled_files f WHERE f.content_hash = p.content_hash ORDER BY path)
			FROM photos p WHERE p.id = $1`, id,
		).Scan(&d.ID, &d.Filename, &d.Width, &d.Height, &d.CapturedAt, &d.TakenAt, &d.CameraModel,
			&d.MimeType, &d.Extension, &d.Size, &d.CameraMake, &d.Latitude, &d.Longitude, &exif,
			&d.HasOriginal, &d.IndexedAt, &d.Paths)
		if err != nil {
			return err
		}
		results := []Result{d.Result}
		if err := attachTags(ctx, tx, results); err != nil {
			return err
		}
		d.Tags = results[0].Tags
		return nil
	})
	if err != nil {
		return nil, notFound(err)
	}
	if exif != nil {
		if err := json.Unmarshal(exif, &d.Exif); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// Embedding returns a photo's stored embedding, for "more like this".
func (s *Store) Embedding(ctx context.Context, id string) ([]float32, error) {
	var text string
	err := s.pool.QueryRow(ctx, "SELECT embedding::text FROM photos WHERE id = $1", id).Scan(&text)
	if err != nil {
		return nil, notFound(err)
	}
	var v []float32
	if err := json.Unmarshal([]byte(text), &v); err != nil { // pgvector text is a JSON array
		return nil, err
	}
	return v, nil
}

// Files says where a photo's bytes can be read from.
type Files struct {
	ThumbnailKey string
	OriginalKey  string // empty when the original isn't in object storage
	MimeType     string
	Paths        []string // local source paths
}

// Files returns the storage keys and local paths of a photo.
func (s *Store) Files(ctx context.Context, id string) (*Files, error) {
	var f Files
	var original *string
	err := s.pool.QueryRow(ctx, `
		SELECT p.thumbnail_key, p.original_key, p.mime_type,
		       ARRAY(SELECT path FROM crawled_files c WHERE c.content_hash = p.content_hash ORDER BY path)
		FROM photos p WHERE p.id = $1`, id).Scan(&f.ThumbnailKey, &original, &f.MimeType, &f.Paths)
	if err != nil {
		return nil, notFound(err)
	}
	if original != nil {
		f.OriginalKey = *original
	}
	return &f, nil
}

// normalizeTags lowercases, trims and de-duplicates tag names.
func normalizeTags(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// notFound maps "no row", "bad id" and "missing reference" errors to
// ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "22P02", "23503": // invalid uuid text, foreign key violation
			return ErrNotFound
		}
	}
	return err
}
