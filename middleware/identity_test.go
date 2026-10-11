// Package middleware is the subject; this file covers identity derivation
// from the Tailscale identity headers injected by tailscale serve
// (https://tailscale.com/s/serve-headers, golink-ux4).
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/holi0317/golink/middleware"
)

// requester captures the identity the middleware chain derived.
type requesterHandler struct {
	login string
	name  string
	found bool
}

func (h *requesterHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	h.login, h.name, h.found = middleware.Requester(r)
}

// TestRequester runs the given headers through TailscaleIdentity and checks
// what Requester reports.
func TestTailscaleIdentity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		login, dis string // header values
		// wantLogin/wantName: derived identity. found = wantLogin != "".
		wantLogin string
		wantName  string
	}{
		{
			name:      "serve-injected identity",
			login:     "alice@example.com",
			dis:       "Alice Architect",
			wantLogin: "alice@example.com",
			wantName:  "Alice Architect",
		},
		{
			name:      "RFC2047 Q-encoded display name",
			login:     "ferris@example.com",
			dis:       "=?utf-8?q?Ferris_B=C3=BCller?=",
			wantLogin: "ferris@example.com",
			wantName:  "Ferris Büller",
		},
		{
			name:      "tagged device / funnel: no identity headers",
			wantLogin: "",
			wantName:  "",
		},
		{
			name:      "blank login despite display name",
			login:     "",
			dis:       "Alice Architect",
			wantLogin: "",
			wantName:  "",
		},
		{
			// mime decodes Q-words leniently: invalid chars come through
			// as their raw bytes rather than an error.
			name:      "Q-encoded invalid chars decode to raw bytes",
			login:     "=?utf-8?q?=FF=FEgarbage?=",
			wantLogin: "\xff\xfegarbage",
			wantName:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inner := &requesterHandler{}
			h := middleware.TailscaleIdentity()(inner)
			r := httptest.NewRequest("GET", "/", nil)
			if tc.login != "" {
				r.Header.Set("Tailscale-User-Login", tc.login)
			}
			if tc.dis != "" {
				r.Header.Set("Tailscale-User-Name", tc.dis)
			}
			h.ServeHTTP(httptest.NewRecorder(), r)

			wantFound := tc.wantLogin != ""
			if wantFound != inner.found {
				t.Fatalf("found mismatch: want %v, got %v", wantFound, inner.found)
			}
			if inner.login != tc.wantLogin {
				t.Fatalf("login: want %q, got %q", tc.wantLogin, inner.login)
			}
			if inner.name != tc.wantName {
				t.Fatalf("name: want %q, got %q", tc.wantName, inner.name)
			}
		})
	}
}

func TestRequesterWithoutMiddleware(t *testing.T) {
	t.Parallel()
	inner := &requesterHandler{}
	// No TailscaleIdentity installed: Requester must report anonymous, not
	// trust client-supplied headers on their own.
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Tailscale-User-Login", "evil@example.com")
	inner.ServeHTTP(httptest.NewRecorder(), r)
	if inner.found {
		t.Fatalf("want anonymous, got login %q", inner.login)
	}
}
