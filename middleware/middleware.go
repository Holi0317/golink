// Package middleware implements the golink HTTP middlewares.
package middleware

import (
	"context"
	"mime"
	"net/http"
	"time"

	"github.com/chainguard-dev/clog"
)

// Middleware wraps an http.Handler with cross-cutting behavior.
type Middleware = func(http.Handler) http.Handler

// ClogProvider returns a middleware that layers the base logger onto each
// request context, so handlers can read it back with clog.FromContext(r.Context()).
//
// The incoming request context is preserved, so client-disconnect cancellation
// keeps working; only the logger is layered on top.
func ClogProvider(logger *clog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(clog.WithLogger(r.Context(), logger)))
		})
	}
}

// RequestLogger returns a slog-based middleware that emits one record per
// request with method, path, response status, and duration.
//
// It reads the contextual logger from the request context, so in the
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

// identity is the requester identity derived from the Tailscale identity
// headers, carried in the request context and read back with Requester.
type identity struct {
	login string
	name  string
}

func (i identity) ok() bool { return i.login != "" }

// Requester returns the requester's Tailscale login and display name injected
// by tailscale serve. ok is false for anonymous requests: no
// TailscaleIdentity middleware in the chain, tagged-device or Funnel traffic
// (tailscale serve does not populate identity headers there), or a hit that
// bypassed tailscale serve entirely.
func Requester(r *http.Request) (login, name string, ok bool) {
	id, present := r.Context().Value(identity{}).(identity)
	if !present || !id.ok() {
		return "", "", false
	}
	return id.login, id.name, true
}

// TailscaleIdentity returns a middleware that converts the Tailscale identity
// headers injected by tailscale serve into the requester identity exposed
// through Requester.
//
// Trust: tailscale serve strips client-sent Tailscale-User-Login/Name on
// ingress and injects the real values (see tailscale.com/s/serve-headers), so
// a populated header chain proves the request terminated at serve. That
// guarantee only covers paths that terminate at serve: the backend must not
// be reachable by any other route (LAN callers, or a substitute proxy), or
// identities are trivially forgeable. The doc-recommended posture is to bind
// the listener to 127.0.0.1, which is golink's default.
//
// Deployments with their own reverse proxy chain (caddy and friends) carry no
// Tailscale-User-Login at all: --tailscale-identity must be left off there,
// and the middleware is not installed when the flag is off, making Requester
// report anonymous.
//
// Identity is absent (anonymous) for tagged-device and Funnel traffic, since
// tailscale serve does not populate the headers there. Values may use
// RFC2047 "Q" encoding for non-ASCII; that is decoded on read.
func TailscaleIdentity() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			login, _ := decodeIdentityHeader(r.Header, "Tailscale-User-Login")
			name, _ := decodeIdentityHeader(r.Header, "Tailscale-User-Name")
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identity{}, identity{login: login, name: name})))
		})
	}
}

// decodeIdentityHeader reads the first occurrence of a header and undoes
// RFC2047 header-word encoding. mime decodes Q-words leniently (invalid
// bytes pass through as raw data), so this only errors on structurally
// broken words; the raw value is returned in that case.
func decodeIdentityHeader(h http.Header, key string) (string, bool) {
	values := h.Values(key)
	if len(values) == 0 || values[0] == "" {
		return "", false
	}
	dec := mime.WordDecoder{}
	decoded, err := dec.DecodeHeader(values[0])
	if err != nil {
		return values[0], true
	}
	return decoded, true
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
