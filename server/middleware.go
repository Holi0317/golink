package server

import (
	"context"
	"net/http"

	"github.com/chainguard-dev/clog"
)

// ContextProvider returns a middleware that threads the process-level logger
// carried by ctx into each request context, so handlers can read it back with
// clog.FromContext(r.Context()) (golink-ddl).
//
// The incoming request context is preserved, so client-disconnect cancellation
// keeps working; only the logger is layered on top.
func ContextProvider(ctx context.Context) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(clog.WithLogger(r.Context(), clog.FromContext(ctx))))
		})
	}
}
