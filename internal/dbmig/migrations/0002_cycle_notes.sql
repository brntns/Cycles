CREATE TABLE IF NOT EXISTS cycle_notes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_id    UUID NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    text        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS cycle_notes_by_cycle
    ON cycle_notes (cycle_id, created_at DESC);

-- target_weeks is a living estimate; new cycles start at 1 week and grow.
ALTER TABLE cycles ALTER COLUMN target_weeks SET DEFAULT 1;
