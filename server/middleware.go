package server

import (
	"net/http"

	"github.com/chainguard-dev/clog"
)

// ClogProvider returns a middleware that layers the base logger onto each
// request context, so handlers can read it back with clog.FromContext(r.Context()).
//
// The incoming request context is preserved, so client-disconnect cancellation
// keeps working; only the logger is layered on top.
func ClogProvider(logger *clog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(clog.WithLogger(r.Context(), logger)))
		})
	}
}
