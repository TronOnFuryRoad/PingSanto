-- 0001_create_upgrade_tables.sql
BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS agent_upgrade_plans (
    agent_id TEXT PRIMARY KEY,
    channel TEXT NOT NULL,
    version TEXT NOT NULL,
    artifact_url TEXT NOT NULL,
    artifact_sha256 TEXT NOT NULL,
    artifact_signature_url TEXT NOT NULL,
    force_apply BOOLEAN NOT NULL DEFAULT FALSE,
    schedule_earliest TIMESTAMPTZ NULL,
    schedule_latest TIMESTAMPTZ NULL,
    paused BOOLEAN NOT NULL DEFAULT FALSE,
    notes TEXT NULL,
    etag TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_upgrade_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id TEXT NOT NULL,
    channel TEXT NOT NULL,
    target_version TEXT NOT NULL,
    previous_version TEXT NULL,
    status TEXT NOT NULL,
    message TEXT NULL,
    details JSONB NULL,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_upgrade_history_agent_time
    ON agent_upgrade_history(agent_id, completed_at DESC);

COMMIT;
