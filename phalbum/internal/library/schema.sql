-- Indexed photos, their embeddings and tags. Applied at startup, so every
-- statement must be idempotent. Source paths live in crawled_files (joined
-- on content_hash), so they aren't duplicated here.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS photos (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    content_hash  text NOT NULL UNIQUE,
    original_key  text,                 -- S3 key; NULL unless originals are stored
    thumbnail_key text NOT NULL,        -- S3 key
    filename      text NOT NULL,
    mime_type     text NOT NULL,
    extension     text NOT NULL,
    size          bigint NOT NULL,
    width         int NOT NULL,         -- after EXIF orientation
    height        int NOT NULL,
    taken_at      timestamptz,
    camera_make   text,
    camera_model  text,
    latitude      double precision,
    longitude     double precision,
    exif          jsonb,
    embedding     vector(512) NOT NULL,
    model         text NOT NULL,        -- embedding model, for future re-indexing
    indexed_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS photos_embedding_idx ON photos USING hnsw (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS photos_taken_at_idx ON photos (taken_at);

CREATE TABLE IF NOT EXISTS tags (
    id   bigserial PRIMARY KEY,
    name text NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS tags_name_idx ON tags (lower(name));

CREATE TABLE IF NOT EXISTS photo_tags (
    photo_id   uuid NOT NULL REFERENCES photos ON DELETE CASCADE,
    tag_id     bigint NOT NULL REFERENCES tags ON DELETE CASCADE,
    source     text NOT NULL CHECK (source IN ('path', 'user', 'auto')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, tag_id)
);
