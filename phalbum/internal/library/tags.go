package library

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrTagExists is returned when renaming a tag to another tag's name.
var ErrTagExists = errors.New("a tag with that name already exists")

// ErrEmptyTag is returned for blank tag names.
var ErrEmptyTag = errors.New("tag name is empty")

// Tag is a tag with the number of photos carrying it.
type Tag struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Photos int    `json:"photos"`
}

// ListTags returns all tags, alphabetically.
func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, count(pt.photo_id)
		FROM tags t LEFT JOIN photo_tags pt ON pt.tag_id = t.id
		GROUP BY t.id ORDER BY lower(t.name)`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Tag])
}

// CreateTag creates a tag, or returns the existing one with the same name
// (case-insensitive).
func (s *Store) CreateTag(ctx context.Context, name string) (Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Tag{}, ErrEmptyTag
	}
	var t Tag
	err := s.pool.QueryRow(ctx, `
		WITH t AS (
			INSERT INTO tags (name) VALUES ($1)
			ON CONFLICT (lower(name)) DO UPDATE SET name = tags.name
			RETURNING id, name
		)
		SELECT t.id, t.name, (SELECT count(*) FROM photo_tags WHERE tag_id = t.id) FROM t`, name,
	).Scan(&t.ID, &t.Name, &t.Photos)
	return t, err
}

// RenameTag changes a tag's name. Changing only its case is allowed.
func (s *Store) RenameTag(ctx context.Context, id int64, name string) (Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Tag{}, ErrEmptyTag
	}
	var t Tag
	err := s.pool.QueryRow(ctx, `
		UPDATE tags SET name = $2 WHERE id = $1
		RETURNING id, name, (SELECT count(*) FROM photo_tags WHERE tag_id = $1)`, id, name,
	).Scan(&t.ID, &t.Name, &t.Photos)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique violation
		return Tag{}, ErrTagExists
	}
	if err != nil {
		return Tag{}, notFound(err)
	}
	return t, nil
}

// DeleteTag deletes a tag and removes it from all photos.
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM tags WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TagPhoto adds a tag (created if needed) to a photo as a user tag and
// returns it.
func (s *Store) TagPhoto(ctx context.Context, photoID, name string) (TagRef, error) {
	if strings.TrimSpace(name) == "" {
		return TagRef{}, ErrEmptyTag
	}
	var ref TagRef
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := addTag(ctx, tx, photoID, name, SourceUser); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT t.id, t.name, pt.source FROM photo_tags pt JOIN tags t ON t.id = pt.tag_id
			WHERE pt.photo_id = $1 AND lower(t.name) = lower($2)`, photoID, strings.TrimSpace(name),
		).Scan(&ref.ID, &ref.Name, &ref.Source)
	})
	return ref, notFound(err)
}

// UntagPhoto removes a tag from a photo. The tag itself is kept.
func (s *Store) UntagPhoto(ctx context.Context, photoID string, tagID int64) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM photo_tags WHERE photo_id = $1 AND tag_id = $2", photoID, tagID)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TagPhotos adds tags (by name, created if needed) to and removes tags (by
// id) from several photos in one transaction. It fails with ErrNotFound,
// changing nothing, if any photo doesn't exist.
func (s *Store) TagPhotos(ctx context.Context, photoIDs []string, add []string, remove []int64) error {
	if len(photoIDs) == 0 {
		return nil
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var found int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM photos WHERE id = ANY($1::uuid[])", photoIDs).Scan(&found); err != nil {
			return err
		}
		if found != len(uniq(photoIDs)) {
			return ErrNotFound
		}
		for _, id := range photoIDs {
			for _, name := range add {
				if err := addTag(ctx, tx, id, name, SourceUser); err != nil {
					return err
				}
			}
		}
		if len(remove) > 0 {
			_, err := tx.Exec(ctx, "DELETE FROM photo_tags WHERE photo_id = ANY($1::uuid[]) AND tag_id = ANY($2)", photoIDs, remove)
			return err
		}
		return nil
	})
	return notFound(err)
}

func uniq(ids []string) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return m
}
