// Package middleware provides the golink HTTP middlewares.
package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
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

// ClientIPKey is the context key under which RealIP stores the requester IP.
type clientIPKey struct{}

// ClientIP returns the requester IP the RealIP middleware derived, or the
// transport address when the middleware is not installed.
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok && ip != "" {
		return ip
	}
	return remoteAddrIP(r)
}

func remoteAddrIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RealIP returns a middleware that derives the requester IP for handlers,
// accounting for proxies in front of golink.
//
// hops is the number of trusted proxy hops between the client and golink
// (e.g. 1 when mounted behind a single caddy). Each proxy appends the peer it
// saw to X-Forwarded-For, so the client IP is the entry that many places from
// the right. With fewer trusted entries than hops — or no X-Forwarded-For at
// all, such as a direct connection — the transport address is used and any
// client-supplied forwarding data is treated as untrusted.
//
// The result is stored in the request context and read back with ClientIP.
//
// Hop counting cannot distinguish a forged X-Forwarded-For on a connection
// that bypasses the trusted proxies, so clients MUST NOT be able to reach
// golink directly; restrict the listener to the proxy network.
//
// In the middleware list it must wrap the handlers that need the IP (i.e.
// come after logging, which does not need it).
func RealIP(hops uint) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ipFor(r, hops))))
		})
	}
}

// ipFor computes the requester IP per the RealIP rules above.
func ipFor(r *http.Request, hops uint) string {
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return remoteAddrIP(r)
	}

	// A single header may carry a comma- and space-separated list. Flatten
	// all headers into one ordered list of hops, leftmost first.
	var entries []string
	for _, header := range xff {
		entries = append(entries, strings.Split(header, ",")...)
	}
	for i := range entries {
		entries[i] = strings.TrimSpace(entries[i])
	}
	// Drop empty segments: they are malformed and carry no usable address.
	usable := make([]string, 0, len(entries))
	for _, e := range entries {
		if e != "" {
			usable = append(usable, e)
		}
	}
	// The rightmost entry was appended by the nearest trusted proxy, so with
	// hops=N the client IP sits N places from the right. Fewer entries than
	// trusted hops means a trusted proxy did not append anything; treat the
	// request as direct rather than trusting client-supplied data.
	if int(hops) > len(usable) {
		return remoteAddrIP(r)
	}
	return usable[len(usable)-int(hops)]
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
