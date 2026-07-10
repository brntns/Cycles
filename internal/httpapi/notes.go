package httpapi

import (
	"errors"
	"net/http"

	"cycles/internal/cyclesvc"
)

type createNoteRequest struct {
	Text string `json:"text"`
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req createNoteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	n, err := s.cycles.AddNote(r.Context(), id, req.Text)
	if err != nil {
		switch {
		case errors.Is(err, cyclesvc.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "cycle not found")
		case errors.Is(err, cyclesvc.ErrNoteEmpty):
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	notes, err := s.cycles.ListNotes(r.Context(), id, r.URL.Query().Get("since"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	noteID := r.PathValue("noteId")
	if err := s.cycles.DeleteNote(r.Context(), id, noteID); err != nil {
		if errors.Is(err, cyclesvc.ErrNoteNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "note not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
