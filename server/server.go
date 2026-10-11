// Package server wires the golink HTTP surface: it composes the redirect
// service, the settings app, and the middlewares into the top-level handler.
package server

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holi0317/golink/db/generated"
	"github.com/holi0317/golink/middleware"
	"github.com/holi0317/golink/redirect"
	"github.com/holi0317/golink/settings"
)

// New returns the top-level HTTP handler for the golink service.
//
// The connection pool is the first parameter as a DI seam: it feeds the
// handlers' dependencies instead of reaching for globals. The middlewares are
// composed in reverse application order: the first middleware sits outermost
// and sees every request first.
//
// Routes:
//   - GET /: redirect to the settings app at /_.
//   - GET /{slug} and GET /{slug}/{rest...}: core redirect service; 302 to
//     the URL stored in the database, not-found page on a miss. Template
//     links (golink-9l6) substitute {0}, {1}, ... references with the path
//     fragments in {rest...}.
//   - /_/*: the settings app (golink-71w for the templ UI).
func New(pool *pgxpool.Pool, mws ...middleware.Middleware) http.Handler {
	queries := db.New(pool)
	redir := redirect.Handler{Queries: queries}
	app := settings.App{Queries: queries}

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
	set := http.StripPrefix("/_", app.Mux())
	redebounce := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/_/", http.StatusTemporaryRedirect)
	}
	mux.HandleFunc("GET /_", redebounce)
	mux.HandleFunc("POST /_", redebounce)
	mux.Handle("GET /_/{path...}", set)
	mux.Handle("POST /_/{path...}", set)
	mux.Handle("GET /{slug}", &redir)
	// Longer paths hit a template link's fragment route; ServeMux resolves
	// the two patterns above by specificity, so single-segment requests
	// still take /{slug}.
	mux.Handle("GET /{slug}/{rest...}", &redir)
	// With both redirect routes covered, this catch-all only serves "/"
	// itself ({$} handles "/" too, so this is effectively unreachable; kept
	// as the not-found floor for paths like "//" and malformed ones).
	mux.HandleFunc("GET /", redir.NotFound(""))

	var h http.Handler = mux
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
