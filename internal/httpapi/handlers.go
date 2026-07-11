package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"cycles/internal/cyclesvc"
	"cycles/internal/model"
	"cycles/internal/questions"
	"cycles/internal/reviews"
)

// --- auth ---

type loginRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	token, expires, err := s.authSvc.Login(r.Context(), clientIP(r), req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
		return
	}

	s.authSvc.SetCookie(w, token, expires)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("cycle_session")
	if err == nil {
		_ = s.authSvc.Logout(r.Context(), cookie.Value)
	}
	s.authSvc.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- status ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	active, err := s.cycles.ActiveCycle(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	lastWeekly, err := s.reviews.LatestWeekly(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	weeklyDue, err := reviews.WeeklyDue(lastWeekly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	lastQuarterly, err := s.reviews.LatestQuarterly(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	baseline := earliestActivityDate(active, lastWeekly)
	quarterlyDue, err := reviews.QuarterlyDue(lastQuarterly, baseline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	streak, err := s.reviews.Streak(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	var daysSince *int
	var lastReviewDate *string
	if lastWeekly != nil {
		lastReviewDate = &lastWeekly.Date
		if last, err := time.Parse("2006-01-02", lastWeekly.Date); err == nil {
			d := int(time.Now().UTC().Truncate(24*time.Hour).Sub(last).Hours() / 24)
			daysSince = &d
		}
	}

	var thisWeekNextStep, thisWeekFridayShow *string
	if lastWeekly != nil && active != nil && lastWeekly.CycleID != nil && *lastWeekly.CycleID == active.ID {
		thisWeekNextStep = &lastWeekly.NextStep
		thisWeekFridayShow = &lastWeekly.FridayShow
	}

	writeJSON(w, http.StatusOK, model.Status{
		ActiveCycle:         active,
		DaysSinceLastReview: daysSince,
		LastReviewDate:      lastReviewDate,
		WeeklyReviewDue:     weeklyDue,
		QuarterlyReviewDue:  quarterlyDue,
		ReviewStreak:        streak,
		ThisWeekNextStep:    thisWeekNextStep,
		ThisWeekFridayShow:  thisWeekFridayShow,
	})
}

func earliestActivityDate(active *model.Cycle, lastWeekly *model.WeeklyReview) *string {
	if active != nil {
		return &active.StartedAt
	}
	if lastWeekly != nil {
		return &lastWeekly.Date
	}
	return nil
}

// --- cycles ---

type createCycleRequest struct {
	Title       string `json:"title"`
	Intent      string `json:"intent"`
	TargetWeeks int    `json:"target_weeks"`
	ShowPlan    string `json:"show_plan"`
}

func (s *Server) handleCreateCycle(w http.ResponseWriter, r *http.Request) {
	var req createCycleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	c, err := s.cycles.Create(r.Context(), cyclesvc.CreateInput{
		Title:       req.Title,
		Intent:      req.Intent,
		TargetWeeks: req.TargetWeeks,
		ShowPlan:    req.ShowPlan,
	})
	if err != nil {
		if errors.Is(err, cyclesvc.ErrActiveCycleExists) {
			writeError(w, http.StatusConflict, "active_cycle_exists", "only one cycle may be active at a time; finish or bury it first")
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListCycles(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	list, err := s.cycles.List(r.Context(), state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetCycle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.cycles.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, cyclesvc.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "cycle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type patchCycleRequest struct {
	Title       *string           `json:"title"`
	Intent      *string           `json:"intent"`
	TargetWeeks *int              `json:"target_weeks"`
	ShowPlan    *string           `json:"show_plan"`
	ArtifactURL *string           `json:"artifact_url"`
	BrainDump   *string           `json:"brain_dump"`
	State       *model.CycleState `json:"state"`
}

func (s *Server) handlePatchCycle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req patchCycleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.State != nil && !req.State.Valid() {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown state")
		return
	}

	c, err := s.cycles.Patch(r.Context(), id, cyclesvc.PatchInput{
		Title:       req.Title,
		Intent:      req.Intent,
		TargetWeeks: req.TargetWeeks,
		ShowPlan:    req.ShowPlan,
		ArtifactURL: req.ArtifactURL,
		BrainDump:   req.BrainDump,
		State:       req.State,
	})
	if err != nil {
		switch {
		case errors.Is(err, cyclesvc.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "cycle not found")
		case errors.Is(err, cyclesvc.ErrInvalidTransition):
			writeError(w, http.StatusUnprocessableEntity, "invalid_transition", "that state change isn't allowed from the current state")
		case errors.Is(err, cyclesvc.ErrMissingArtifact):
			writeError(w, http.StatusUnprocessableEntity, "missing_artifact", err.Error())
		case errors.Is(err, cyclesvc.ErrMissingBrainDump):
			writeError(w, http.StatusUnprocessableEntity, "missing_brain_dump", err.Error())
		default:
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// --- weekly reviews ---

type createWeeklyReviewRequest struct {
	Date       string          `json:"date"`
	CycleID    *string         `json:"cycle_id"`
	Answers    json.RawMessage `json:"answers"`
	NextStep   string          `json:"next_step"`
	FridayShow string          `json:"friday_show"`
}

func (s *Server) handleCreateWeeklyReview(w http.ResponseWriter, r *http.Request) {
	var req createWeeklyReviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	cycleID := req.CycleID
	if cycleID == nil {
		if active, err := s.cycles.ActiveCycle(r.Context()); err == nil && active != nil {
			cycleID = &active.ID
		}
	}

	wr, err := s.reviews.CreateWeekly(r.Context(), reviews.WeeklyInput{
		Date:       req.Date,
		CycleID:    cycleID,
		Answers:    req.Answers,
		NextStep:   req.NextStep,
		FridayShow: req.FridayShow,
	})
	if err != nil {
		if errors.Is(err, reviews.ErrNextStepRequired) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, wr)
}

func (s *Server) handleListWeeklyReviews(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	list, err := s.reviews.ListWeekly(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// --- quarterly reviews ---

type createQuarterlyReviewRequest struct {
	Date    string          `json:"date"`
	Answers json.RawMessage `json:"answers"`
}

func (s *Server) handleCreateQuarterlyReview(w http.ResponseWriter, r *http.Request) {
	var req createQuarterlyReviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	qr, err := s.reviews.CreateQuarterly(r.Context(), reviews.QuarterlyInput{
		Date:    req.Date,
		Answers: req.Answers,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, qr)
}

func (s *Server) handleListQuarterlyReviews(w http.ResponseWriter, r *http.Request) {
	list, err := s.reviews.ListQuarterly(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// --- parked questions ---

type createQuestionRequest struct {
	Question string `json:"question"`
}

func (s *Server) handleCreateQuestion(w http.ResponseWriter, r *http.Request) {
	var req createQuestionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	q, err := s.questions.Create(r.Context(), req.Question)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

func (s *Server) handleListQuestions(w http.ResponseWriter, r *http.Request) {
	var list []*model.ParkedQuestion
	var err error
	if status := r.URL.Query().Get("status"); status != "" {
		list, err = s.questions.ListByStatus(r.Context(), model.QuestionStatus(status))
	} else {
		list, err = s.questions.List(r.Context())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type patchQuestionRequest struct {
	Status     *model.QuestionStatus `json:"status"`
	AppendNote *string               `json:"append_note"`
}

func (s *Server) handlePatchQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req patchQuestionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	q, err := s.questions.Patch(r.Context(), id, questions.PatchInput{
		Status:     req.Status,
		AppendNote: req.AppendNote,
	})
	if err != nil {
		switch {
		case errors.Is(err, questions.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "parked question not found")
		case errors.Is(err, questions.ErrInvalidStatus):
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// --- export ---

type exportPayload struct {
	ExportedAt       time.Time                `json:"exported_at"`
	Cycles           []*model.Cycle           `json:"cycles"`
	CycleNotes       []*model.CycleNote       `json:"cycle_notes"`
	Ideas            []*model.Idea            `json:"ideas"`
	WeeklyReviews    []*model.WeeklyReview    `json:"weekly_reviews"`
	QuarterlyReviews []*model.QuarterlyReview `json:"quarterly_reviews"`
	ParkedQuestions  []*model.ParkedQuestion  `json:"parked_questions"`
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	allCycles, err := s.cycles.List(ctx, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	allNotes, err := s.cycles.ListAllNotes(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	allIdeas, err := s.ideas.List(ctx, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	allWeekly, err := s.reviews.ListWeekly(ctx, 1<<30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	allQuarterly, err := s.reviews.ListQuarterly(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	allQuestions, err := s.questions.List(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, exportPayload{
		ExportedAt:       time.Now().UTC(),
		Cycles:           allCycles,
		CycleNotes:       allNotes,
		Ideas:            allIdeas,
		WeeklyReviews:    allWeekly,
		QuarterlyReviews: allQuarterly,
		ParkedQuestions:  allQuestions,
	})
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
