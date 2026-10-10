package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chainguard-dev/clog"
)

// A handler receiving a request through ClogProvider must see a logger in
// its request context.
func TestClogProviderInjectsLogger(t *testing.T) {
	baseLogger := clog.New(clog.NewHandler(nil))

	seen := make(chan *clog.Logger, 1)
	h := ClogProvider(baseLogger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- clog.FromContext(r.Context())
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got := <-seen; got == nil || got.Logger != baseLogger.Logger {
		t.Fatal("handler did not see the base logger in the request context")
	}
}

// The middleware must not replace the request context wholesale: it would
// sever client-disconnect cancellation that handlers rely on. Pre-cancel a
// request context and require the wrapped handler to still observe Done.
func TestClogProviderPreservesRequestCancellation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	reqCtx, cancel := context.WithCancel(req.Context())
	cancel() // deliver an already-canceled request context

	h := ClogProvider(clog.New(clog.NewHandler(nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		default:
			t.Error("request cancellation was lost through the middleware")
		}
	}))

	h.ServeHTTP(httptest.NewRecorder(), req.WithContext(reqCtx))
}
