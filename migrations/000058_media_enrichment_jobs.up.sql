-- Media enrichment job foundation for Inbox/Scout style async media processing.
-- Large media stays in artifact storage; this schema stores references,
-- provider/model metadata, retry state, and idempotency keys.

CREATE TABLE IF NOT EXISTS media_artifacts (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    source_event_id TEXT NOT NULL,
    media_kind      VARCHAR(32) NOT NULL,
    artifact_hash   TEXT NOT NULL,
    storage_uri     TEXT NOT NULL,
    mime_type       TEXT NOT NULL DEFAULT '',
    original_name   TEXT NOT NULL DEFAULT '',
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, artifact_hash)
);

CREATE INDEX IF NOT EXISTS idx_media_artifacts_source
  ON media_artifacts(tenant_id, source_event_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_media_artifacts_kind
  ON media_artifacts(tenant_id, media_kind, created_at DESC);

CREATE TABLE IF NOT EXISTS media_enrichment_jobs (
    id              TEXT PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    source_event_id TEXT NOT NULL,
    media_kind      VARCHAR(32) NOT NULL,
    artifact_hash   TEXT NOT NULL,
    provider        VARCHAR(64) NOT NULL,
    model           TEXT NOT NULL,
    mode            VARCHAR(64) NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempt_count   INT NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,
    UNIQUE (tenant_id, idempotency_key),
    CHECK (status IN ('pending', 'running', 'done', 'failed_retryable', 'failed_terminal')),
    CHECK (attempt_count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_media_jobs_status
  ON media_enrichment_jobs(tenant_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_media_jobs_source
  ON media_enrichment_jobs(tenant_id, source_event_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_media_jobs_artifact
  ON media_enrichment_jobs(tenant_id, artifact_hash, created_at DESC);
