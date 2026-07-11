package httpapi

import (
	"errors"
	"net/http"

	"cycles/internal/cyclesvc"
)

type createEntryRequest struct {
	Text string `json:"text"`
}

// handleCreateEntry adds a user-written update to a cycle's timeline.
// Only kind=update can be created here; system and review entries are
// generated server-side.
func (s *Server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req createEntryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	e, err := s.cycles.AddUpdate(r.Context(), id, req.Text)
	if err != nil {
		switch {
		case errors.Is(err, cyclesvc.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "cycle not found")
		case errors.Is(err, cyclesvc.ErrEntryEmpty):
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entries, err := s.cycles.ListEntries(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entryID := r.PathValue("entryId")
	if err := s.cycles.DeleteEntry(r.Context(), id, entryID); err != nil {
		switch {
		case errors.Is(err, cyclesvc.ErrEntryNotFound):
			writeError(w, http.StatusNotFound, "not_found", "entry not found")
		case errors.Is(err, cyclesvc.ErrEntryImmutable):
			writeError(w, http.StatusUnprocessableEntity, "entry_immutable", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
