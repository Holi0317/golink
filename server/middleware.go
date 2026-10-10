package server

import (
	"net/http"
	"time"

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

// RequestLogger returns a slog-based middleware that emits one record per
// request with method, path, response status, and duration.
//
// It reads the contextual logger from the request context, so in New's
// middleware list it must come after ClogProvider (which sits outermost and
// installs the logger).
func RequestLogger() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := clog.FromContext(r.Context())
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// statusRecorder records the status code a handler answered with, so the
// request log can carry it.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// status reports the recorded status; handlers that never call WriteHeader
// (plain writes) implicitly answer 200.
func (r *statusRecorder) status() int {
	if r.code == 0 {
		return http.StatusOK
	}
	return r.code
}
