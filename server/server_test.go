package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlugPlaceholderUntilRedirectService(t *testing.T) {
	for _, path := range []string{"/", "/greet", "/nested/path"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			New().ServeHTTP(rec, req)

			if rec.Code != http.StatusNotImplemented {
				t.Errorf("GET %s: got status %d, want %d", path, rec.Code, http.StatusNotImplemented)
			}
		})
	}
}

func TestUnsupportedMethodsRejected(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/", nil)
			New().ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /: got status %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}
