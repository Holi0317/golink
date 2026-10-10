// Package server implements the golink HTTP surface: the top-level mux and
// its handlers.
package server

import "net/http"

// Middleware wraps an http.Handler with cross-cutting behavior.
type Middleware = func(http.Handler) http.Handler

// New returns the top-level HTTP handler for the golink service, composed of
// the middleware in reverse application order: the first middleware sits
// outermost and sees every request first.
//
// The /{slug} catch-all is a placeholder that answers 501 until the core
// redirect service (slug resolution, not-found page, request logging) lands.
func New(mws ...Middleware) http.Handler {
	var h = mux()
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "golink: slug resolution not implemented yet", http.StatusNotImplemented)
	})
	return mux
}
