BEGIN;

CREATE TABLE IF NOT EXISTS controller_settings (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE,
    notify_on_publish BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO controller_settings (id, notify_on_publish)
VALUES (TRUE, TRUE)
ON CONFLICT (id) DO NOTHING;

COMMIT;
