// Package model holds the domain types shared across the API.
package model

import (
	"encoding/json"
	"time"
)

type CycleState string

const (
	StateBuilding      CycleState = "building"
	StateUnderstanding CycleState = "understanding"
	StateShowing       CycleState = "showing"
	StateCompleted     CycleState = "completed"
	StateBuried        CycleState = "buried"
)

func (s CycleState) Terminal() bool {
	return s == StateCompleted || s == StateBuried
}

func (s CycleState) Valid() bool {
	switch s {
	case StateBuilding, StateUnderstanding, StateShowing, StateCompleted, StateBuried:
		return true
	}
	return false
}

type Cycle struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Intent      string     `json:"intent"`
	State       CycleState `json:"state"`
	StartedAt   string     `json:"started_at"`
	TargetWeeks int        `json:"target_weeks"`
	ShowPlan    string     `json:"show_plan"`
	ArtifactURL *string    `json:"artifact_url"`
	BrainDump   *string    `json:"brain_dump"`
	EndedAt     *string    `json:"ended_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CycleNote struct {
	ID        string    `json:"id"`
	CycleID   string    `json:"cycle_id"`
	CreatedAt time.Time `json:"created_at"`
	Text      string    `json:"text"`
}

type IdeaStatus string

const (
	IdeaOpen      IdeaStatus = "open"
	IdeaPromoted  IdeaStatus = "promoted"
	IdeaDiscarded IdeaStatus = "discarded"
)

type Idea struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Note            *string    `json:"note"`
	CreatedAt       time.Time  `json:"created_at"`
	Status          IdeaStatus `json:"status"`
	PromotedCycleID *string    `json:"promoted_cycle_id"`
}

type QuestionStatus string

const (
	QuestionParked   QuestionStatus = "parked"
	QuestionAnswered QuestionStatus = "answered"
	QuestionDropped  QuestionStatus = "dropped"
)

type ParkedQuestionNote struct {
	Date string `json:"date"`
	Note string `json:"note"`
}

type ParkedQuestion struct {
	ID        string               `json:"id"`
	Question  string               `json:"question"`
	CreatedAt string               `json:"created_at"`
	Status    QuestionStatus       `json:"status"`
	Notes     []ParkedQuestionNote `json:"notes"`
}

type WeeklyReview struct {
	ID         string          `json:"id"`
	Date       string          `json:"date"`
	CycleID    *string         `json:"cycle_id"`
	Answers    json.RawMessage `json:"answers"`
	NextStep   string          `json:"next_step"`
	FridayShow string          `json:"friday_show"`
	CreatedAt  time.Time       `json:"created_at"`
}

type QuarterlyReview struct {
	ID        string          `json:"id"`
	Date      string          `json:"date"`
	Answers   json.RawMessage `json:"answers"`
	CreatedAt time.Time       `json:"created_at"`
}

type Status struct {
	ActiveCycle         *Cycle  `json:"active_cycle"`
	DaysSinceLastReview *int    `json:"days_since_last_review"`
	LastReviewDate      *string `json:"last_review_date"`
	WeeklyReviewDue     bool    `json:"weekly_review_due"`
	QuarterlyReviewDue  bool    `json:"quarterly_review_due"`
	ReviewStreak        int     `json:"review_streak"`
	ThisWeekNextStep    *string `json:"this_week_next_step"`
	ThisWeekFridayShow  *string `json:"this_week_friday_show"`
}
