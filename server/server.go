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
//   - GET /{slug}: core redirect service; 302 to the URL stored in the
//     database, not-found page on a miss.
//   - /_/: the settings app (placeholder until golink-71w).
func New(pool *pgxpool.Pool, mws ...Middleware) http.Handler {
	s := &Server{queries: db.New(pool)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.notFound(""))
	mux.HandleFunc("GET /{slug}", s.redirect)
	// Longer paths have no single slug to resolve; ServeMux routes them here
	// because the more specific patterns above do not match.
	mux.HandleFunc("GET /", s.notFound(""))
	// The settings app owns everything under /_/; it is not built yet, so a
	// placeholder answers for now (golink-71w). It is registered as GET-only
	// because a method-less wildcard here conflicts with the GET catch-all
	// below in ServeMux's precedence rules.
	mux.HandleFunc("GET /_/", s.settingsPlaceholder)

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
// go link. PLACEHOLDER until golink-71w: the settings UI will provide a
// templ page with a working create form; this just ports the ask over a
// plain HTML response.
func (s *Server) notFound(slug string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		// golink-71w replaces this markup with the templ not-found page and
		// links the ask to the create form.
		body := "<!DOCTYPE html>\n" +
			"<html><head><title>Not found — golink</title></head><body>\n" +
			"<h1>No go link for “" + slug + "” yet</h1>\n" +
			"<p>Do you want to create a new go link for it? Head to the\n" +
			"settings app (create form coming soon).</p>\n" +
			"</body></html>\n"
		_, _ = w.Write([]byte(body))
	}
}

// settingsPlaceholder stands in for the settings app until golink-71w
// delivers the templ-based UI at /_/*.
func (s *Server) settingsPlaceholder(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "golink: settings app not implemented yet", http.StatusNotImplemented)
}
