-- Cycle Timeline (spec amendment): the notes table becomes the timeline.
-- Existing CycleNotes migrate to kind=update; system and review entries
-- join them in one chronological stream per cycle.

ALTER TABLE cycle_notes RENAME TO timeline_entries;
ALTER INDEX cycle_notes_by_cycle RENAME TO timeline_entries_by_cycle;

ALTER TABLE timeline_entries
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'update'
        CHECK (kind IN ('update', 'system', 'review')),
    ADD COLUMN ref_id UUID;

-- Backfill system entries for lifecycle events that already happened, so
-- past cycles read as complete stories. Intermediate state transitions
-- were never recorded and cannot be reconstructed.
INSERT INTO timeline_entries (cycle_id, created_at, kind, text)
SELECT id, created_at, 'system', 'Cycle created' FROM cycles;

INSERT INTO timeline_entries (cycle_id, created_at, kind, text)
SELECT id, updated_at, 'system',
       CASE state WHEN 'completed' THEN 'Cycle completed' ELSE 'Cycle buried' END
FROM cycles WHERE state IN ('completed', 'buried');

INSERT INTO timeline_entries (cycle_id, created_at, kind, text, ref_id)
SELECT cycle_id, created_at, 'review', 'Weekly review', id
FROM weekly_reviews WHERE cycle_id IS NOT NULL;
