// Package server implements the golink HTTP surface: the top-level mux and
// its handlers.
package server

import "net/http"

// New returns the top-level HTTP handler for the golink service.
//
// The /{slug} catch-all is a placeholder that answers 501 until the core
// redirect service (slug resolution, not-found page, request logging) lands.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "golink: slug resolution not implemented yet", http.StatusNotImplemented)
	})
	return mux
}
