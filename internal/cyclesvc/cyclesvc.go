// Package cyclesvc implements the Cycle domain: creation, listing, and the
// state machine transitions enforced server-side.
package cyclesvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"varde/internal/model"
)

func todayISO() string {
	return time.Now().UTC().Format("2006-01-02")
}

var (
	ErrActiveCycleExists = errors.New("an active cycle already exists")
	ErrNotFound          = errors.New("cycle not found")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrMissingArtifact   = errors.New("artifact_url is required to complete a cycle")
	ErrMissingBrainDump  = errors.New("brain_dump is required for this transition")
)

// allowedTransitions maps a state to the set of states it may move to.
var allowedTransitions = map[model.CycleState][]model.CycleState{
	model.StateBuilding:      {model.StateUnderstanding, model.StateBuried},
	model.StateUnderstanding: {model.StateShowing, model.StateBuried},
	model.StateShowing:       {model.StateCompleted, model.StateBuried},
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type CreateInput struct {
	Title       string
	Intent      string
	TargetWeeks int
	ShowPlan    string
}

func (s *Store) Create(ctx context.Context, in CreateInput) (*model.Cycle, error) {
	if in.TargetWeeks == 0 {
		in.TargetWeeks = 1
	}
	if in.TargetWeeks < 1 || in.TargetWeeks > 16 {
		return nil, fmt.Errorf("target_weeks must be between 1 and 16")
	}
	if in.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO cycles (title, intent, state, target_weeks, show_plan)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, title, intent, state, started_at, target_weeks, show_plan,
		          artifact_url, brain_dump, ended_at, created_at, updated_at
	`, in.Title, in.Intent, model.StateBuilding, in.TargetWeeks, in.ShowPlan)

	c, err := scanCycle(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrActiveCycleExists
		}
		return nil, err
	}
	if err := addSystemEntry(ctx, tx, c.ID, "Cycle created"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) List(ctx context.Context, state string) ([]*model.Cycle, error) {
	var rows pgx.Rows
	var err error
	if state != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT id, title, intent, state, started_at, target_weeks, show_plan,
			       artifact_url, brain_dump, ended_at, created_at, updated_at
			FROM cycles WHERE state = $1 ORDER BY created_at DESC
		`, state)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, title, intent, state, started_at, target_weeks, show_plan,
			       artifact_url, brain_dump, ended_at, created_at, updated_at
			FROM cycles ORDER BY created_at DESC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.Cycle
	for rows.Next() {
		c, err := scanCycle(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (*model.Cycle, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, title, intent, state, started_at, target_weeks, show_plan,
		       artifact_url, brain_dump, ended_at, created_at, updated_at
		FROM cycles WHERE id = $1
	`, id)
	c, err := scanCycle(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

// ActiveCycle returns the current non-terminal cycle, or nil if none exists.
func (s *Store) ActiveCycle(ctx context.Context) (*model.Cycle, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, title, intent, state, started_at, target_weeks, show_plan,
		       artifact_url, brain_dump, ended_at, created_at, updated_at
		FROM cycles WHERE state NOT IN ('completed', 'buried')
		ORDER BY created_at DESC LIMIT 1
	`)
	c, err := scanCycle(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return c, nil
}

type PatchInput struct {
	Title       *string
	Intent      *string
	TargetWeeks *int
	ShowPlan    *string
	ArtifactURL *string
	BrainDump   *string
	State       *model.CycleState
}

func (s *Store) Patch(ctx context.Context, id string, in PatchInput) (*model.Cycle, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	artifactURL := current.ArtifactURL
	brainDump := current.BrainDump
	if in.ArtifactURL != nil {
		artifactURL = in.ArtifactURL
	}
	if in.BrainDump != nil {
		brainDump = in.BrainDump
	}

	newState := current.State
	var endedAt *string
	endedAtSet := false

	if in.State != nil && *in.State != current.State {
		if current.State.Terminal() {
			return nil, ErrInvalidTransition
		}
		allowed := false
		for _, s := range allowedTransitions[current.State] {
			if s == *in.State {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrInvalidTransition
		}
		if *in.State == model.StateCompleted {
			if artifactURL == nil || *artifactURL == "" {
				return nil, ErrMissingArtifact
			}
			if brainDump == nil || *brainDump == "" {
				return nil, ErrMissingBrainDump
			}
		}
		if *in.State == model.StateBuried {
			if brainDump == nil || *brainDump == "" {
				return nil, ErrMissingBrainDump
			}
		}
		newState = *in.State
		if newState.Terminal() {
			today := todayISO()
			endedAt = &today
			endedAtSet = true
		}
	}

	title := current.Title
	if in.Title != nil {
		title = *in.Title
	}
	intent := current.Intent
	if in.Intent != nil {
		intent = *in.Intent
	}
	targetWeeks := current.TargetWeeks
	if in.TargetWeeks != nil {
		targetWeeks = *in.TargetWeeks
		if targetWeeks < 1 || targetWeeks > 16 {
			return nil, fmt.Errorf("target_weeks must be between 1 and 16")
		}
	}
	showPlan := current.ShowPlan
	if in.ShowPlan != nil {
		showPlan = *in.ShowPlan
	}

	// System entries for the timeline: estimate first, then the state
	// change, so a terminal event ends up newest.
	var systemTexts []string
	if targetWeeks != current.TargetWeeks {
		systemTexts = append(systemTexts, "Estimate changed to ~"+weeksLabel(targetWeeks))
	}
	if newState != current.State {
		switch newState {
		case model.StateCompleted:
			systemTexts = append(systemTexts, "Cycle completed")
		case model.StateBuried:
			systemTexts = append(systemTexts, "Cycle buried")
		default:
			systemTexts = append(systemTexts, "Moved to "+string(newState))
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var row pgx.Row
	if endedAtSet {
		row = tx.QueryRow(ctx, `
			UPDATE cycles SET title=$1, intent=$2, state=$3, target_weeks=$4, show_plan=$5,
			       artifact_url=$6, brain_dump=$7, ended_at=$8, updated_at=now()
			WHERE id=$9
			RETURNING id, title, intent, state, started_at, target_weeks, show_plan,
			          artifact_url, brain_dump, ended_at, created_at, updated_at
		`, title, intent, newState, targetWeeks, showPlan, artifactURL, brainDump, endedAt, id)
	} else {
		row = tx.QueryRow(ctx, `
			UPDATE cycles SET title=$1, intent=$2, state=$3, target_weeks=$4, show_plan=$5,
			       artifact_url=$6, brain_dump=$7, updated_at=now()
			WHERE id=$8
			RETURNING id, title, intent, state, started_at, target_weeks, show_plan,
			          artifact_url, brain_dump, ended_at, created_at, updated_at
		`, title, intent, newState, targetWeeks, showPlan, artifactURL, brainDump, id)
	}

	c, err := scanCycle(row)
	if err != nil {
		return nil, err
	}
	for _, text := range systemTexts {
		if err := addSystemEntry(ctx, tx, id, text); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func weeksLabel(n int) string {
	if n == 1 {
		return "1 week"
	}
	return fmt.Sprintf("%d weeks", n)
}

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
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
