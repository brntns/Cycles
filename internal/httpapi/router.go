// Package httpapi wires the HTTP surface: routing, auth, and JSON
// marshaling for the Cycle Companion REST API.
package httpapi

import (
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"cycles/internal/auth"
	"cycles/internal/cyclesvc"
	"cycles/internal/ideas"
	"cycles/internal/questions"
	"cycles/internal/reviews"
)

type Server struct {
	pool      *pgxpool.Pool
	authSvc   *auth.Service
	cycles    *cyclesvc.Store
	reviews   *reviews.Store
	questions *questions.Store
	ideas     *ideas.Store
	staticFS  fs.FS
}

func NewServer(pool *pgxpool.Pool, authSvc *auth.Service, staticFS fs.FS) *Server {
	return &Server{
		pool:      pool,
		authSvc:   authSvc,
		cycles:    cyclesvc.NewStore(pool),
		reviews:   reviews.NewStore(pool),
		questions: questions.NewStore(pool),
		ideas:     ideas.NewStore(pool),
		staticFS:  staticFS,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("GET /health", s.handleHealth)

	// Authenticated API
	api := http.NewServeMux()
	api.HandleFunc("POST /auth/logout", s.handleLogout)
	api.HandleFunc("GET /status", s.handleStatus)
	api.HandleFunc("POST /cycles", s.handleCreateCycle)
	api.HandleFunc("GET /cycles", s.handleListCycles)
	api.HandleFunc("GET /cycles/{id}", s.handleGetCycle)
	api.HandleFunc("PATCH /cycles/{id}", s.handlePatchCycle)
	api.HandleFunc("POST /cycles/{id}/notes", s.handleCreateNote)
	api.HandleFunc("GET /cycles/{id}/notes", s.handleListNotes)
	api.HandleFunc("DELETE /cycles/{id}/notes/{noteId}", s.handleDeleteNote)
	api.HandleFunc("POST /reviews/weekly", s.handleCreateWeeklyReview)
	api.HandleFunc("GET /reviews/weekly", s.handleListWeeklyReviews)
	api.HandleFunc("POST /reviews/quarterly", s.handleCreateQuarterlyReview)
	api.HandleFunc("GET /reviews/quarterly", s.handleListQuarterlyReviews)
	api.HandleFunc("GET /questions", s.handleListQuestions)
	api.HandleFunc("POST /questions", s.handleCreateQuestion)
	api.HandleFunc("PATCH /questions/{id}", s.handlePatchQuestion)
	api.HandleFunc("POST /ideas", s.handleCreateIdea)
	api.HandleFunc("GET /ideas", s.handleListIdeas)
	api.HandleFunc("PATCH /ideas/{id}", s.handlePatchIdea)
	api.HandleFunc("POST /ideas/{id}/promote", s.handlePromoteIdea)
	api.HandleFunc("GET /export", s.handleExport)

	mux.Handle("/auth/logout", s.authSvc.Middleware(api))
	mux.Handle("/status", s.authSvc.Middleware(api))
	mux.Handle("/cycles", s.authSvc.Middleware(api))
	mux.Handle("/cycles/", s.authSvc.Middleware(api))
	mux.Handle("/reviews/", s.authSvc.Middleware(api))
	mux.Handle("/questions", s.authSvc.Middleware(api))
	mux.Handle("/questions/", s.authSvc.Middleware(api))
	mux.Handle("/ideas", s.authSvc.Middleware(api))
	mux.Handle("/ideas/", s.authSvc.Middleware(api))
	mux.Handle("/export", s.authSvc.Middleware(api))

	// Static web UI (PWA shell) — everything not matched above.
	mux.Handle("/", http.FileServer(http.FS(s.staticFS)))

	return mux
}
