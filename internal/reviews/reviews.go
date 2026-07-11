// Package reviews implements the weekly and quarterly guided-ritual
// persistence and the due/streak calculations surfaced on /status.
package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cycles/internal/model"
)

var ErrNextStepRequired = errors.New("next_step is required")

const quarterlyPeriod = 12 * 7 * 24 * time.Hour

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type WeeklyInput struct {
	Date       string
	CycleID    *string
	Answers    json.RawMessage
	NextStep   string
	FridayShow string
}

func (s *Store) CreateWeekly(ctx context.Context, in WeeklyInput) (*model.WeeklyReview, error) {
	if in.NextStep == "" {
		return nil, ErrNextStepRequired
	}
	if in.Date == "" {
		in.Date = time.Now().UTC().Format("2006-01-02")
	}
	if len(in.Answers) == 0 {
		in.Answers = json.RawMessage(`{}`)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO weekly_reviews (date, cycle_id, answers, next_step, friday_show)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, date, cycle_id, answers, next_step, friday_show, created_at
	`, in.Date, in.CycleID, in.Answers, in.NextStep, in.FridayShow)

	wr, err := scanWeekly(row)
	if err != nil {
		return nil, err
	}
	// A review of a cycle becomes part of that cycle's timeline, rendered
	// as a richer card inline (kind=review, ref_id → the review).
	if wr.CycleID != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO timeline_entries (cycle_id, kind, text, ref_id)
			VALUES ($1, 'review', 'Weekly review', $2)
		`, *wr.CycleID, wr.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return wr, nil
}

func (s *Store) ListWeekly(ctx context.Context, limit int) ([]*model.WeeklyReview, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, date, cycle_id, answers, next_step, friday_show, created_at
		FROM weekly_reviews ORDER BY date DESC, created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.WeeklyReview
	for rows.Next() {
		wr, err := scanWeekly(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, wr)
	}
	return result, rows.Err()
}

func (s *Store) LatestWeekly(ctx context.Context) (*model.WeeklyReview, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, date, cycle_id, answers, next_step, friday_show, created_at
		FROM weekly_reviews ORDER BY date DESC, created_at DESC LIMIT 1
	`)
	wr, err := scanWeekly(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return wr, nil
}

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanWeekly(row scanner) (*model.WeeklyReview, error) {
	var wr model.WeeklyReview
	var date time.Time
	if err := row.Scan(&wr.ID, &date, &wr.CycleID, &wr.Answers, &wr.NextStep, &wr.FridayShow, &wr.CreatedAt); err != nil {
		return nil, err
	}
	wr.Date = date.Format("2006-01-02")
	return &wr, nil
}

// Streak counts consecutive weekly reviews, newest first, allowing at most
// a 10-day gap between two reviews to still count as "kept the rhythm"
// (reviews land on Sunday but people slip by a day or two).
func (s *Store) Streak(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT date FROM weekly_reviews ORDER BY date DESC LIMIT 200`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var dates []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return 0, err
		}
		dates = append(dates, d)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(dates) == 0 {
		return 0, nil
	}

	streak := 1
	for i := 1; i < len(dates); i++ {
		gap := dates[i-1].Sub(dates[i])
		if gap <= 10*24*time.Hour {
			streak++
		} else {
			break
		}
	}
	return streak, nil
}

type QuarterlyInput struct {
	Date    string
	Answers json.RawMessage
}

func (s *Store) CreateQuarterly(ctx context.Context, in QuarterlyInput) (*model.QuarterlyReview, error) {
	if in.Date == "" {
		in.Date = time.Now().UTC().Format("2006-01-02")
	}
	if len(in.Answers) == 0 {
		in.Answers = json.RawMessage(`{}`)
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO quarterly_reviews (date, answers)
		VALUES ($1, $2)
		RETURNING id, date, answers, created_at
	`, in.Date, in.Answers)

	return scanQuarterly(row)
}

func (s *Store) ListQuarterly(ctx context.Context) ([]*model.QuarterlyReview, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, date, answers, created_at FROM quarterly_reviews ORDER BY date DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.QuarterlyReview
	for rows.Next() {
		qr, err := scanQuarterly(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, qr)
	}
	return result, rows.Err()
}

func (s *Store) LatestQuarterly(ctx context.Context) (*model.QuarterlyReview, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, date, answers, created_at FROM quarterly_reviews ORDER BY date DESC LIMIT 1
	`)
	qr, err := scanQuarterly(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return qr, nil
}

func scanQuarterly(row scanner) (*model.QuarterlyReview, error) {
	var qr model.QuarterlyReview
	var date time.Time
	if err := row.Scan(&qr.ID, &date, &qr.Answers, &qr.CreatedAt); err != nil {
		return nil, err
	}
	qr.Date = date.Format("2006-01-02")
	return &qr, nil
}

// WeeklyDue implements "true if today >= next Sunday since last review".
// With no review yet, there is no baseline to count from, so the ritual is
// considered due immediately — the app should invite a first review rather
// than wait for a Sunday that has no prior anchor.
func WeeklyDue(lastReview *model.WeeklyReview) (bool, error) {
	if lastReview == nil {
		return true, nil
	}
	last, err := time.Parse("2006-01-02", lastReview.Date)
	if err != nil {
		return false, err
	}
	nextSunday := nextSundayAfter(last)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return !today.Before(nextSunday), nil
}

func nextSundayAfter(d time.Time) time.Time {
	next := d.AddDate(0, 0, 1)
	for next.Weekday() != time.Sunday {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// QuarterlyDue implements "the last QuarterlyReview is >=12 weeks old".
// With no quarterly review yet, baselineDate (earliest known activity —
// the first cycle's started_at or first weekly review) anchors the clock;
// if there's no activity at all yet, it isn't due.
func QuarterlyDue(lastQuarterly *model.QuarterlyReview, baselineDate *string) (bool, error) {
	var since time.Time
	if lastQuarterly != nil {
		d, err := time.Parse("2006-01-02", lastQuarterly.Date)
		if err != nil {
			return false, err
		}
		since = d
	} else if baselineDate != nil {
		d, err := time.Parse("2006-01-02", *baselineDate)
		if err != nil {
			return false, err
		}
		since = d
	} else {
		return false, nil
	}
	return time.Now().UTC().Sub(since) >= quarterlyPeriod, nil
}
