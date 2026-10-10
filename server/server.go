// Package server implements the golink HTTP surface: the top-level mux and
// its handlers.
package server

import (
	"errors"
	"net/http"

	"github.com/chainguard-dev/clog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holi0317/golink/server/db"
)

// Middleware wraps an http.Handler with cross-cutting behavior.
type Middleware = func(http.Handler) http.Handler

// Server carries the dependencies the HTTP handlers need. Handlers are
// methods on it, so more dependencies (templ components and the like) are a
// new field plus constructor wiring, and tests can build a Server directly.
type Server struct {
	queries *db.Queries
}

// New returns the top-level HTTP handler for the golink service.
//
// The connection pool is the first parameter as a DI seam: it feeds the
// Server's dependencies instead of reaching for globals. The middlewares are
// composed in reverse application order: the first middleware sits outermost
// and sees every request first.
//
// Routes:
//   - GET /: redirect to the settings app at /_.
//   - GET /{slug}: core redirect service; 302 to the URL stored in the
//     database, not-found page on a miss.
//   - /_/*: the settings app (golink-71w for the templ UI).
func New(pool *pgxpool.Pool, mws ...Middleware) http.Handler {
	s := &Server{queries: db.New(pool)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// The settings app is the front door; / has no meaning other than
		// pointing people there.
		http.Redirect(w, r, "/_/", http.StatusTemporaryRedirect)
	})
	// The settings app is its own mux rooted at /_, mounted with a two-way
	// StripPrefix so its handlers route with plain patterns: "/_/" maps to
	// "/" and "/_/edit/42" to "/edit/42". The bare "/_" is an explicit
	// redirect because the wildcard above does not cover it reliably.
	settings := http.StripPrefix("/_", s.settingsMux())
	redebounce := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/_/", http.StatusTemporaryRedirect)
	}
	mux.HandleFunc("GET /_", redebounce)
	mux.HandleFunc("POST /_", redebounce)
	mux.Handle("GET /_/{path...}", settings)
	mux.Handle("POST /_/{path...}", settings)
	mux.HandleFunc("GET /{slug}", s.redirect)
	// Longer paths have no single slug to resolve; ServeMux routes them here
	// because the more specific patterns above do not match.
	mux.HandleFunc("GET /", s.notFound(""))

	var h http.Handler = mux
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// redirect resolves the request slug and answers 302 with the URL stored in
// the database. A miss renders the not-found page; any other storage error
// is logged and answers 500.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	slug := r.PathValue("slug")
	link, err := s.queries.GetLink(r.Context(), slug)
	if errors.Is(err, pgx.ErrNoRows) {
		s.notFound(slug)(w, r)
		return
	}
	if err != nil {
		log.Errorf("resolving slug %q: %v", slug, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, link.TargetUrl, http.StatusFound)
}

// notFound is the miss page: it asks whether the visitor wants to create the
// go link. An empty slug stands for paths with no slug at all (root,
// nested).
func (s *Server) notFound(slug string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notFoundStatus(w, r, slug)
	}
}
