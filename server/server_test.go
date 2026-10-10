package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
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

// New composes the middleware in the given order: the first argument wraps
// everything and processes requests first, before handlers below it.
func TestNewComposesMiddlewareInOrder(t *testing.T) {
	var order []string
	rec := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := New(rec("outer"), rec("inner"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !slices.Equal(order, []string{"outer", "inner"}) {
		t.Fatalf("middleware order: got %v, want [outer inner]", order)
	}
}
