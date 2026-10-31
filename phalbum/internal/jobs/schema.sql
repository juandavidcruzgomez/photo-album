-- Indexing queue: one job per unique image content. Applied at startup, so
-- every statement must be idempotent.
CREATE TABLE IF NOT EXISTS index_jobs (
    job_id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    content_hash text NOT NULL UNIQUE,          -- sha256 hex
    uri          text NOT NULL,                 -- file:///... for now, s3://... later
    filename     text NOT NULL,
    size         bigint NOT NULL,
    mime_type    text NOT NULL,
    extension    text NOT NULL,
    tags         text[] NOT NULL DEFAULT '{}',
    exif         jsonb,
    status       text NOT NULL DEFAULT 'queued'
                 CHECK (status IN ('queued', 'processing', 'success', 'error')),
    message      text NOT NULL DEFAULT '',
    attempts     int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS index_jobs_queued ON index_jobs (created_at) WHERE status = 'queued';

-- Every image path the crawler has seen, so unchanged files are not re-hashed.
CREATE TABLE IF NOT EXISTS crawled_files (
    path         text PRIMARY KEY,
    size         bigint NOT NULL,
    mod_time     timestamptz NOT NULL,
    content_hash text NOT NULL,
    seen_at      timestamptz NOT NULL DEFAULT now()
);
