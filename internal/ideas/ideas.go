// Package ideas implements the idea backlog: a capture bin for ideas that
// strike mid-cycle, so they can be parked in seconds instead of competing
// with the single active cycle. Deciding happens only at cycle boundaries
// or during reviews — the promote flow enforces the same single-active-
// cycle rule as cycle creation.
package ideas

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"varde/internal/model"
)

var (
	ErrTitleRequired     = errors.New("title is required")
	ErrNotFound          = errors.New("idea not found")
	ErrInvalidStatus     = errors.New("invalid status")
	ErrNotOpen           = errors.New("only an open idea can be promoted")
	ErrActiveCycleExists = errors.New("an active cycle already exists")
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Create(ctx context.Context, title string, note *string) (*model.Idea, error) {
	if title == "" {
		return nil, ErrTitleRequired
	}
	if note != nil && *note == "" {
		note = nil
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ideas (title, note) VALUES ($1, $2)
		RETURNING id, title, note, created_at, status, promoted_cycle_id
	`, title, note)
	return scanIdea(row)
}

func (s *Store) List(ctx context.Context, status string) ([]*model.Idea, error) {
	query := `SELECT id, title, note, created_at, status, promoted_cycle_id
	          FROM ideas ORDER BY created_at DESC`
	args := []any{}
	if status != "" {
		query = `SELECT id, title, note, created_at, status, promoted_cycle_id
		         FROM ideas WHERE status = $1 ORDER BY created_at DESC`
		args = append(args, status)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ideas := []*model.Idea{}
	for rows.Next() {
		idea, err := scanIdea(rows)
		if err != nil {
			return nil, err
		}
		ideas = append(ideas, idea)
	}
	return ideas, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (*model.Idea, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, title, note, created_at, status, promoted_cycle_id
		FROM ideas WHERE id = $1
	`, id)
	idea, err := scanIdea(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return idea, nil
}

type PatchInput struct {
	Title  *string
	Note   *string
	Status *model.IdeaStatus
}

func (s *Store) Patch(ctx context.Context, id string, in PatchInput) (*model.Idea, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	title := current.Title
	if in.Title != nil {
		if *in.Title == "" {
			return nil, ErrTitleRequired
		}
		title = *in.Title
	}

	note := current.Note
	if in.Note != nil {
		if *in.Note == "" {
			note = nil
		} else {
			note = in.Note
		}
	}

	status := current.Status
	if in.Status != nil {
		// promoted is only reachable via the promote endpoint; open ⇄
		// discarded covers capture and the undo of a discard.
		switch *in.Status {
		case model.IdeaOpen, model.IdeaDiscarded:
			status = *in.Status
		default:
			return nil, ErrInvalidStatus
		}
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE ideas SET title = $1, note = $2, status = $3 WHERE id = $4
		RETURNING id, title, note, created_at, status, promoted_cycle_id
	`, title, note, status, id)
	return scanIdea(row)
}

// Promote turns an open idea into the next cycle in one transaction:
// it creates a cycle prefilled with the idea's title and note-as-intent
// (callers may override title/intent/target_weeks/show_plan), marks the
// idea promoted, and links it to the new cycle. The partial unique index
// on cycles rejects this with ErrActiveCycleExists while a cycle is active.
type PromoteInput struct {
	Title       *string
	Intent      *string
	TargetWeeks int
	ShowPlan    string
}

func (s *Store) Promote(ctx context.Context, id string, in PromoteInput) (*model.Idea, *model.Cycle, error) {
	idea, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if idea.Status != model.IdeaOpen {
		return nil, nil, ErrNotOpen
	}

	title := idea.Title
	if in.Title != nil && *in.Title != "" {
		title = *in.Title
	}
	intent := ""
	if idea.Note != nil {
		intent = *idea.Note
	}
	if in.Intent != nil {
		intent = *in.Intent
	}
	weeks := in.TargetWeeks
	if weeks == 0 {
		weeks = 1
	}
	if weeks < 1 || weeks > 16 {
		return nil, nil, errors.New("target_weeks must be between 1 and 16")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	cycleRow := tx.QueryRow(ctx, `
		INSERT INTO cycles (title, intent, state, target_weeks, show_plan)
		VALUES ($1, $2, 'building', $3, $4)
		RETURNING id, title, intent, state, started_at, target_weeks, show_plan,
		          artifact_url, brain_dump, ended_at, created_at, updated_at
	`, title, intent, weeks, in.ShowPlan)

	cycle, err := scanCycle(cycleRow)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, nil, ErrActiveCycleExists
		}
		return nil, nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO timeline_entries (cycle_id, kind, text)
		VALUES ($1, 'system', 'Cycle created')
	`, cycle.ID); err != nil {
		return nil, nil, err
	}

	ideaRow := tx.QueryRow(ctx, `
		UPDATE ideas SET status = 'promoted', promoted_cycle_id = $1 WHERE id = $2
		RETURNING id, title, note, created_at, status, promoted_cycle_id
	`, cycle.ID, id)
	updated, err := scanIdea(ideaRow)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return updated, cycle, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanIdea(row scanner) (*model.Idea, error) {
	var idea model.Idea
	if err := row.Scan(&idea.ID, &idea.Title, &idea.Note, &idea.CreatedAt, &idea.Status, &idea.PromotedCycleID); err != nil {
		return nil, err
	}
	return &idea, nil
}

func scanCycle(row scanner) (*model.Cycle, error) {
	var c model.Cycle
	var startedAt time.Time
	var endedAt *time.Time
	err := row.Scan(&c.ID, &c.Title, &c.Intent, &c.State, &startedAt, &c.TargetWeeks,
		&c.ShowPlan, &c.ArtifactURL, &c.BrainDump, &endedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.StartedAt = startedAt.Format("2006-01-02")
	if endedAt != nil {
		s := endedAt.Format("2006-01-02")
		c.EndedAt = &s
	}
	return &c, nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
