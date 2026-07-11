package cyclesvc

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"cycles/internal/model"
)

var (
	ErrEntryNotFound  = errors.New("entry not found")
	ErrEntryEmpty     = errors.New("text is required")
	ErrEntryImmutable = errors.New("only update entries can be deleted")
)

// querier is satisfied by both *pgxpool.Pool and pgx.Tx, so system entries
// can be written inside the transaction that caused the event.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// AddUpdate appends a user-written update (kind=update) to a cycle's
// timeline. System and review entries are never created through here.
func (s *Store) AddUpdate(ctx context.Context, cycleID, text string) (*model.TimelineEntry, error) {
	if text == "" {
		return nil, ErrEntryEmpty
	}
	// Verify the cycle exists so the caller gets a 404 rather than an FK error.
	if _, err := s.Get(ctx, cycleID); err != nil {
		return nil, err
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO timeline_entries (cycle_id, kind, text)
		VALUES ($1, 'update', $2)
		RETURNING id, cycle_id, created_at, kind, text, ref_id
	`, cycleID, text)

	var e model.TimelineEntry
	if err := row.Scan(&e.ID, &e.CycleID, &e.CreatedAt, &e.Kind, &e.Text, &e.RefID); err != nil {
		return nil, err
	}
	return &e, nil
}

// addSystemEntry records a lifecycle event (cycle created, state change,
// estimate change, completion/burial) as a quiet system entry.
// clock_timestamp() keeps insertion order within one transaction.
func addSystemEntry(ctx context.Context, q querier, cycleID, text string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO timeline_entries (cycle_id, created_at, kind, text)
		VALUES ($1, clock_timestamp(), 'system', $2)
	`, cycleID, text)
	return err
}

// ListEntries returns a cycle's full timeline (all kinds), newest first.
// Review entries come back with their WeeklyReview hydrated.
func (s *Store) ListEntries(ctx context.Context, cycleID string) ([]*model.TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.cycle_id, e.created_at, e.kind, e.text, e.ref_id,
		       w.id, w.date, w.cycle_id, w.answers, w.next_step, w.friday_show, w.created_at
		FROM timeline_entries e
		LEFT JOIN weekly_reviews w ON e.kind = 'review' AND w.id = e.ref_id
		WHERE e.cycle_id = $1
		ORDER BY e.created_at DESC
	`, cycleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []*model.TimelineEntry{}
	for rows.Next() {
		var e model.TimelineEntry
		var rID, rCycleID, rNextStep, rFridayShow *string
		var rDate, rCreatedAt *time.Time
		var rAnswers []byte
		if err := rows.Scan(&e.ID, &e.CycleID, &e.CreatedAt, &e.Kind, &e.Text, &e.RefID,
			&rID, &rDate, &rCycleID, &rAnswers, &rNextStep, &rFridayShow, &rCreatedAt); err != nil {
			return nil, err
		}
		if rID != nil {
			e.Review = &model.WeeklyReview{
				ID:         *rID,
				Date:       rDate.Format("2006-01-02"),
				CycleID:    rCycleID,
				Answers:    rAnswers,
				NextStep:   *rNextStep,
				FridayShow: *rFridayShow,
				CreatedAt:  *rCreatedAt,
			}
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// ListAllEntries returns every timeline entry across all cycles, newest
// first (export).
func (s *Store) ListAllEntries(ctx context.Context) ([]*model.TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, cycle_id, created_at, kind, text, ref_id
		FROM timeline_entries ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []*model.TimelineEntry{}
	for rows.Next() {
		var e model.TimelineEntry
		if err := rows.Scan(&e.ID, &e.CycleID, &e.CreatedAt, &e.Kind, &e.Text, &e.RefID); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// DeleteEntry removes a user-written update. System and review entries are
// part of the cycle's story and cannot be deleted.
func (s *Store) DeleteEntry(ctx context.Context, cycleID, entryID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM timeline_entries WHERE id = $1 AND cycle_id = $2 AND kind = 'update'
	`, entryID, cycleID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM timeline_entries WHERE id = $1 AND cycle_id = $2)
		`, entryID, cycleID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrEntryImmutable
		}
		return ErrEntryNotFound
	}
	return nil
}
