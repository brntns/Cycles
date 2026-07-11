// Package questions implements the parked "big life questions" container,
// which only surfaces for edits during the quarterly review.
package questions

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"varde/internal/model"
)

var (
	ErrQuestionRequired = errors.New("question is required")
	ErrNotFound         = errors.New("parked question not found")
	ErrInvalidStatus    = errors.New("invalid status")
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Create(ctx context.Context, question string) (*model.ParkedQuestion, error) {
	if question == "" {
		return nil, ErrQuestionRequired
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO parked_questions (question, status, notes)
		VALUES ($1, 'parked', '[]'::jsonb)
		RETURNING id, question, created_at, status, notes
	`, question)
	return scan(row)
}

func (s *Store) List(ctx context.Context) ([]*model.ParkedQuestion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, question, created_at, status, notes
		FROM parked_questions ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.ParkedQuestion
	for rows.Next() {
		q, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, q)
	}
	return result, rows.Err()
}

func (s *Store) ListByStatus(ctx context.Context, status model.QuestionStatus) ([]*model.ParkedQuestion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, question, created_at, status, notes
		FROM parked_questions WHERE status = $1 ORDER BY created_at ASC
	`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.ParkedQuestion
	for rows.Next() {
		q, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, q)
	}
	return result, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (*model.ParkedQuestion, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, question, created_at, status, notes FROM parked_questions WHERE id = $1
	`, id)
	q, err := scan(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return q, nil
}

type PatchInput struct {
	Status     *model.QuestionStatus
	AppendNote *string
}

func (s *Store) Patch(ctx context.Context, id string, in PatchInput) (*model.ParkedQuestion, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	status := current.Status
	if in.Status != nil {
		switch *in.Status {
		case model.QuestionParked, model.QuestionAnswered, model.QuestionDropped:
			status = *in.Status
		default:
			return nil, ErrInvalidStatus
		}
	}

	notes := current.Notes
	if in.AppendNote != nil && *in.AppendNote != "" {
		notes = append(notes, model.ParkedQuestionNote{
			Date: time.Now().UTC().Format("2006-01-02"),
			Note: *in.AppendNote,
		})
	}
	notesJSON, err := json.Marshal(notes)
	if err != nil {
		return nil, err
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE parked_questions SET status = $1, notes = $2 WHERE id = $3
		RETURNING id, question, created_at, status, notes
	`, status, notesJSON, id)
	return scan(row)
}

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scan(row scanner) (*model.ParkedQuestion, error) {
	var q model.ParkedQuestion
	var notesJSON []byte
	var createdAt time.Time
	if err := row.Scan(&q.ID, &q.Question, &createdAt, &q.Status, &notesJSON); err != nil {
		return nil, err
	}
	q.CreatedAt = createdAt.Format("2006-01-02")
	if len(notesJSON) > 0 {
		if err := json.Unmarshal(notesJSON, &q.Notes); err != nil {
			return nil, err
		}
	}
	return &q, nil
}
