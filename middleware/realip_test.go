// Package middleware is the subject; this file covers X-Forwarded-For
// handling behind a configurable number of trusted proxy hops (golink-9zl).
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/holi0317/golink/middleware"
)

// handlerHTML captures the requester IP the middleware derived.
type handlerIP struct {
	ip string
}

func (h *handlerIP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ip = middleware.ClientIP(r)
}

// Test: for each case, hits the wrapped handler with the given headers and
// checks the derived client IP.
func TestRealIP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		hops         uint
		xff          []string
		remoteAddr   string
		wantDirectIP string // ClientIP result
	}{
		{
			name:         "direct connection, hops=0",
			hops:         0,
			xff:          nil,
			remoteAddr:   "10.0.0.1:51000",
			wantDirectIP: "10.0.0.1",
		},
		{
			name: "single caddy hop",
			hops: 1,
			xff:  []string{"203.0.113.7"},
			// caddy's own address as the transport peer
			remoteAddr:   "10.0.0.2:51000",
			wantDirectIP: "203.0.113.7",
		},
		{
			name: "single caddy hop, client-supplied chain accepted for leftmost",
			hops: 1,
			xff:  []string{"203.0.113.7, 198.51.100.9"},
			// caddy is the last hop; it forwarded the client chain intact
			remoteAddr:   "10.0.0.2:51000",
			wantDirectIP: "198.51.100.9",
		},
		{
			name:         "no XFF despite hops, direct",
			hops:         1,
			remoteAddr:   "10.0.0.1:51000",
			wantDirectIP: "10.0.0.1",
		},
		{
			// fewer entries than trusted hops: a trusted proxy did not
			// append anything, so do not trust the client-supplied value.
			// (A direct attacker forging exactly N entries is out of scope
			// for header-based reasoning: golink must not be reachable
			// directly, only through the trusted proxies.)
			name:         "short XFF does not let attacker spoof",
			hops:         2,
			xff:          []string{"6.6.6.6"},
			remoteAddr:   "10.0.0.1:51000",
			wantDirectIP: "10.0.0.1",
		},
		{
			name:         "comma-separated list in one header",
			hops:         1,
			xff:          []string{"203.0.113.7,198.51.100.9"},
			remoteAddr:   "10.0.0.2:51000",
			wantDirectIP: "198.51.100.9",
		},
		{
			name:         "two hops",
			hops:         2,
			xff:          []string{"203.0.113.7, 198.51.100.9"},
			remoteAddr:   "10.0.0.3:51000",
			wantDirectIP: "203.0.113.7",
		},
		{
			// client forges entries, but both trusted proxies appended
			// theirs; the entry recorded by the outermost trusted proxy
			// (CDN-observed transport) wins.
			name:         "two hops, three entries forged",
			hops:         2,
			xff:          []string{"6.6.6.6, 203.0.113.7, 198.51.100.9"},
			remoteAddr:   "10.0.0.3:51000",
			wantDirectIP: "203.0.113.7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inner := &handlerIP{}
			h := middleware.RealIP(tc.hops)(inner)
			r := httptest.NewRequest("GET", "/", nil)
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			r.RemoteAddr = tc.remoteAddr
			h.ServeHTTP(httptest.NewRecorder(), r)

			if inner.ip != tc.wantDirectIP {
				t.Fatalf("want %q, got %q", tc.wantDirectIP, inner.ip)
			}
		})
	}
}

func TestClientIPWithoutMiddleware(t *testing.T) {
	t.Parallel()
	inner := &handlerIP{}
	// No RealIP installed: ClientIP falls back to the transport address.
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	r.RemoteAddr = "10.0.0.1:51000"
	inner.ServeHTTP(httptest.NewRecorder(), r)
	if inner.ip != "10.0.0.1" {
		t.Fatalf("want fallback 10.0.0.1, got %q", inner.ip)
	}
}
