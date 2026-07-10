CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS sessions (
    token       TEXT PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS cycles (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title         TEXT NOT NULL,
    intent        TEXT NOT NULL DEFAULT '',
    state         TEXT NOT NULL CHECK (state IN ('building', 'understanding', 'showing', 'completed', 'buried')),
    started_at    DATE NOT NULL DEFAULT CURRENT_DATE,
    target_weeks  INTEGER NOT NULL DEFAULT 8 CHECK (target_weeks BETWEEN 1 AND 16),
    show_plan     TEXT NOT NULL DEFAULT '',
    artifact_url  TEXT,
    brain_dump    TEXT,
    ended_at      DATE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Enforce "only one cycle may be in a non-terminal state at a time".
CREATE UNIQUE INDEX IF NOT EXISTS one_active_cycle
    ON cycles ((true))
    WHERE state NOT IN ('completed', 'buried');

CREATE TABLE IF NOT EXISTS weekly_reviews (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    date         DATE NOT NULL DEFAULT CURRENT_DATE,
    cycle_id     UUID REFERENCES cycles(id),
    answers      JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_step    TEXT NOT NULL,
    friday_show  TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS parked_questions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question    TEXT NOT NULL,
    created_at  DATE NOT NULL DEFAULT CURRENT_DATE,
    status      TEXT NOT NULL DEFAULT 'parked' CHECK (status IN ('parked', 'answered', 'dropped')),
    notes       JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS quarterly_reviews (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    date        DATE NOT NULL DEFAULT CURRENT_DATE,
    answers     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
