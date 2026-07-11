package httpapi

import (
	"errors"
	"net/http"

	"varde/internal/ideas"
	"varde/internal/model"
)

type createIdeaRequest struct {
	Title string  `json:"title"`
	Note  *string `json:"note"`
}

func (s *Server) handleCreateIdea(w http.ResponseWriter, r *http.Request) {
	var req createIdeaRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	idea, err := s.ideas.Create(r.Context(), req.Title, req.Note)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, idea)
}

func (s *Server) handleListIdeas(w http.ResponseWriter, r *http.Request) {
	list, err := s.ideas.List(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type patchIdeaRequest struct {
	Title  *string           `json:"title"`
	Note   *string           `json:"note"`
	Status *model.IdeaStatus `json:"status"`
}

func (s *Server) handlePatchIdea(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req patchIdeaRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	idea, err := s.ideas.Patch(r.Context(), id, ideas.PatchInput{
		Title:  req.Title,
		Note:   req.Note,
		Status: req.Status,
	})
	if err != nil {
		switch {
		case errors.Is(err, ideas.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "idea not found")
		case errors.Is(err, ideas.ErrInvalidStatus), errors.Is(err, ideas.ErrTitleRequired):
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, idea)
}

type promoteIdeaRequest struct {
	Title       *string `json:"title"`
	Intent      *string `json:"intent"`
	TargetWeeks int     `json:"target_weeks"`
	ShowPlan    string  `json:"show_plan"`
}

type promoteIdeaResponse struct {
	Idea  *model.Idea  `json:"idea"`
	Cycle *model.Cycle `json:"cycle"`
}

func (s *Server) handlePromoteIdea(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req promoteIdeaRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
	}

	idea, cycle, err := s.ideas.Promote(r.Context(), id, ideas.PromoteInput{
		Title:       req.Title,
		Intent:      req.Intent,
		TargetWeeks: req.TargetWeeks,
		ShowPlan:    req.ShowPlan,
	})
	if err != nil {
		switch {
		case errors.Is(err, ideas.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "idea not found")
		case errors.Is(err, ideas.ErrNotOpen):
			writeError(w, http.StatusUnprocessableEntity, "not_open", err.Error())
		case errors.Is(err, ideas.ErrActiveCycleExists):
			writeError(w, http.StatusConflict, "active_cycle_exists", "only one cycle may be active at a time; finish or bury it first")
		default:
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, promoteIdeaResponse{Idea: idea, Cycle: cycle})
}
