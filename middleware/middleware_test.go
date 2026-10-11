package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

// RequestLogger emits one record per request carrying method, path, and the
// status the handler answered with.
func TestRequestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := clog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	})

	h := ClogProvider(logger)(RequestLogger()(inner))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/greet", nil))

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("request log is not a JSON object: %v (log: %q)", err, buf.String())
	}
	if got, want := record["msg"], "request"; got != want {
		t.Errorf("msg: got %v, want %q", got, want)
	}
	if got, want := record["method"], "GET"; got != want {
		t.Errorf("method: got %v, want %q", got, want)
	}
	if got, want := record["path"], "/greet"; got != want {
		t.Errorf("path: got %v, want %q", got, want)
	}
	if got, want := record["status"], float64(http.StatusFound); got != want {
		t.Errorf("status: got %v, want %v", got, want)
	}
	if ms, ok := record["duration_ms"].(float64); !ok || ms < 0 {
		t.Errorf("duration_ms: got %v, want a non-negative number", record["duration_ms"])
	}
}

// A handler that only writes a body (never calls WriteHeader) implicitly
// answers 200, and the request log must record that.
func TestRequestLoggerDefaultsStatusOK(t *testing.T) {
	var buf bytes.Buffer
	logger := clog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hi"))
	})

	h := ClogProvider(logger)(RequestLogger()(inner))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("request log is not a JSON object: %v", err)
	}
	if got, want := record["status"], float64(http.StatusOK); got != want {
		t.Errorf("status: got %v, want %v", got, want)
	}
}
