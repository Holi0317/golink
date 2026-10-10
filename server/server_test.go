package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holi0317/golink/server/db"
)

// testPool opens a pool against the database named by DATABASE_URL; the test
// skips when the variable is not exported (mise exports it for local runs
// against the pitchfork dev postgres).
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping database-backed test")
	}
	pool, err := NewPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedLink inserts a link row and schedules its removal, so tests never leak
// state into the developer database.
func seedLink(t *testing.T, pool *pgxpool.Pool, slug, targetURL string) {
	t.Helper()

	queries := db.New(pool)
	if _, err := queries.CreateLink(context.Background(), db.CreateLinkParams{
		Slug:      slug,
		TargetUrl: targetURL,
		CreatedBy: "server_test",
	}); err != nil {
		t.Fatalf("seeding link %q: %v", slug, err)
	}
	t.Cleanup(func() { _, _ = queries.DeleteLink(context.Background(), slug) })
}

// A stored slug resolves to a 302 with the stored URL as Location.
func TestRedirectSendsStoredURL(t *testing.T) {
	pool := testPool(t)
	seedLink(t, pool, "greet", "https://example.com/hello")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/greet", nil)
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("GET /greet: got status %d, want %d", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://example.com/hello"; got != want {
		t.Errorf("Location: got %q, want %q", got, want)
	}
}

// An unknown slug answers the not-found page (404) after asking the visitor
// whether they want to create the go link.
func TestUnknownSlugSendsNotFoundPage(t *testing.T) {
	pool := testPool(t) // no rows seeded; the slug never exists

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/greet", nil)
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /greet: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
	for _, fragment := range []string{"greet", "create"} {
		if !strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("not-found page: body %q misses %q", rec.Body.String(), fragment)
		}
	}
}

// Paths without a single slug (root, nested paths) also answer the
// not-found page rather than leaking into storage.
func TestSluglessPathSendsNotFoundPage(t *testing.T) {
	pool := testPool(t)

	for _, path := range []string{"/", "/nested/path"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			New(pool).ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s: got status %d, want %d", path, rec.Code, http.StatusNotFound)
			}
		})
	}
}

// The settings app owns /_/*; until golink-71w it answers as a placeholder.
func TestSettingsAppPlaceholder(t *testing.T) {
	pool := testPool(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_/links", nil)
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("GET /_/links: got status %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestUnsupportedMethodsRejected(t *testing.T) {
	pool := testPool(t)
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/", nil)
			New(pool).ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /: got status %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

// New composes the middleware in the given order: the first argument wraps
// everything and processes requests first, before handlers below it.
func TestNewComposesMiddlewareInOrder(t *testing.T) {
	pool := testPool(t)

	var order []string
	rec := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := New(pool, rec("outer"), rec("inner"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !slices.Equal(order, []string{"outer", "inner"}) {
		t.Fatalf("middleware order: got %v, want [outer inner]", order)
	}
}
