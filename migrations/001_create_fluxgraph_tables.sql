-- FluxGraph schema migration: 001_create_fluxgraph_tables.sql
-- Apply with: psql $DATABASE_URL -f migrations/001_create_fluxgraph_tables.sql

BEGIN;

-- Sessions registry tracks lifecycle per tenant.
CREATE TABLE IF NOT EXISTS sessions (
    session_id      TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL DEFAULT 'default',
    status          TEXT        NOT NULL DEFAULT 'Running',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_tenant ON sessions (tenant_id);

-- agent_states stores the latest full AgentState snapshot (one row per session).
-- version acts as an optimistic lock: concurrent writers must read and increment.
CREATE TABLE IF NOT EXISTS agent_states (
    session_id   TEXT        PRIMARY KEY REFERENCES sessions(session_id) ON DELETE CASCADE,
    state_json   JSONB       NOT NULL,
    version      BIGINT      NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- checkpoints are immutable historical snapshots, one per Save() call.
CREATE TABLE IF NOT EXISTS checkpoints (
    checkpoint_id  TEXT        PRIMARY KEY,
    session_id     TEXT        NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    node_id        TEXT,                        -- node active when checkpoint was created
    state_json     JSONB       NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_checkpoints_session_time
    ON checkpoints (session_id, created_at DESC);

-- messages stores individual conversation turns for efficient append and
-- sliding-window queries without re-serialising the full state blob.
CREATE TABLE IF NOT EXISTS messages (
    id           BIGSERIAL   PRIMARY KEY,
    session_id   TEXT        NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    tenant_id    TEXT        NOT NULL DEFAULT 'default',
    role         TEXT        NOT NULL,
    content_json JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_messages_session_time
    ON messages (session_id, created_at DESC);

COMMIT;
