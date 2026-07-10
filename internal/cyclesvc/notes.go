package cyclesvc

import (
	"context"
	"errors"

	"cycles/internal/model"
)

var (
	ErrNoteNotFound = errors.New("note not found")
	ErrNoteEmpty    = errors.New("text is required")
)

func (s *Store) AddNote(ctx context.Context, cycleID, text string) (*model.CycleNote, error) {
	if text == "" {
		return nil, ErrNoteEmpty
	}
	// Verify the cycle exists so the caller gets a 404 rather than an FK error.
	if _, err := s.Get(ctx, cycleID); err != nil {
		return nil, err
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO cycle_notes (cycle_id, text)
		VALUES ($1, $2)
		RETURNING id, cycle_id, created_at, text
	`, cycleID, text)

	var n model.CycleNote
	if err := row.Scan(&n.ID, &n.CycleID, &n.CreatedAt, &n.Text); err != nil {
		return nil, err
	}
	return &n, nil
}

// ListNotes returns a cycle's notes newest first. A non-empty since
// (YYYY-MM-DD) limits the list to notes created on or after that date.
func (s *Store) ListNotes(ctx context.Context, cycleID, since string) ([]*model.CycleNote, error) {
	query := `
		SELECT id, cycle_id, created_at, text FROM cycle_notes
		WHERE cycle_id = $1 ORDER BY created_at DESC`
	args := []any{cycleID}
	if since != "" {
		query = `
			SELECT id, cycle_id, created_at, text FROM cycle_notes
			WHERE cycle_id = $1 AND created_at >= $2::date ORDER BY created_at DESC`
		args = append(args, since)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []*model.CycleNote{}
	for rows.Next() {
		var n model.CycleNote
		if err := rows.Scan(&n.ID, &n.CycleID, &n.CreatedAt, &n.Text); err != nil {
			return nil, err
		}
		notes = append(notes, &n)
	}
	return notes, rows.Err()
}

// ListAllNotes returns every note across all cycles, newest first (export).
func (s *Store) ListAllNotes(ctx context.Context) ([]*model.CycleNote, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, cycle_id, created_at, text FROM cycle_notes ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []*model.CycleNote{}
	for rows.Next() {
		var n model.CycleNote
		if err := rows.Scan(&n.ID, &n.CycleID, &n.CreatedAt, &n.Text); err != nil {
			return nil, err
		}
		notes = append(notes, &n)
	}
	return notes, rows.Err()
}

func (s *Store) DeleteNote(ctx context.Context, cycleID, noteID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM cycle_notes WHERE id = $1 AND cycle_id = $2
	`, noteID, cycleID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoteNotFound
	}
	return nil
}
