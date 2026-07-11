CREATE TABLE IF NOT EXISTS ideas (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title              TEXT NOT NULL,
    note               TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    status             TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'promoted', 'discarded')),
    promoted_cycle_id  UUID REFERENCES cycles(id)
);

CREATE INDEX IF NOT EXISTS ideas_by_status
    ON ideas (status, created_at DESC);
