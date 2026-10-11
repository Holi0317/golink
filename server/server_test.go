package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	// pgtestdb provisions per-test template-cloned databases; the stdlib
	// pgx driver backs its database/sql connections.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/peterldowns/pgtestdb"

	"github.com/holi0317/golink/db/generated"
	"github.com/holi0317/golink/db/migrations"
	"github.com/holi0317/golink/middleware"
)

// testPool provisions a fresh postgres database for this test. pgtestdb
// migrates a template database once (embedded dbmate migrations via the
// dbmatemigrator adapter) and clones it per test, so every test starts from
// the exact prod schema with no cross-test state; the instance is dropped on
// success. Server coordinates come from DATABASE_URL (mise exports it
// locally against the pitchfork dev postgres; CI provides a postgres
// service). Skips when DATABASE_URL is not exported.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping database-backed test")
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parsing DATABASE_URL: %v", err)
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	password, _ := u.User.Password()
	conf := pgtestdb.Config{
		// pgx (github.com/jackc/pgx/v5/stdlib) registered below.
		DriverName: "pgx",
		User:       u.User.Username(),
		Password:   password,
		Host:       u.Hostname(),
		Port:       port,
		Options:    "sslmode=disable",
	}
	// dbmateMigrator reuses the exact dbmate setup prod uses; hash covers
	// every embedded migration file so the template is rebuilt only when
	// migrations change.
	cfg := pgtestdb.Custom(t, conf, dbmateMigrator{})
	pool, err := pgxpool.New(context.Background(), cfg.URL())
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// dbmateMigrator implements pgtestdb.Migrator on top of db/migrations.New —
// the same embedded dbmate engine the prod `golink migrate` command uses.
type dbmateMigrator struct{}

// Hash derives the template database name from the migration contents, so
// the template is re-migrated only when migrations change.
func (dbmateMigrator) Hash() (string, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return "", fmt.Errorf("listing embedded migrations: %w", err)
	}
	h := sha256.New()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sql, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", entry.Name(), err)
		}
		line := fmt.Sprintf("%s\x00%d\x00", entry.Name(), len(sql))
		h.Write([]byte(line))
		h.Write(sql)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (dbmateMigrator) Migrate(_ context.Context, _ *sql.DB, conf pgtestdb.Config) error {
	db, err := migrations.New(conf.URL())
	if err != nil {
		return err
	}
	return db.Migrate()
}

// seedLink inserts a link row for a test to work with. The database the test
// runs against is a per-test template clone, dropped when the test ends, so
// no cleanup is needed. It returns the created row, so tests can address it
// by id.
func seedLink(t *testing.T, pool *pgxpool.Pool, slug, targetURL string) db.Link {
	t.Helper()

	queries := db.New(pool)
	link, err := queries.CreateLink(context.Background(), db.CreateLinkParams{
		Slug:      slug,
		TargetUrl: targetURL,
		CreatedBy: "server_test",
	})
	if err != nil {
		t.Fatalf("seeding link %q: %v", slug, err)
	}
	return link
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

// The root redirects to the settings app at /_, which is the front door.
func TestRootRedirectsToSettings(t *testing.T) {
	pool := testPool(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("GET /: got status %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
	if got, want := rec.Header().Get("Location"), "/_/"; got != want {
		t.Errorf("GET / Location: got %q, want %q", got, want)
	}
}

// Nested paths have no single slug to resolve; they answer the not-found
// page rather than leaking into storage.
func TestNestedPathSendsNotFoundPage(t *testing.T) {
	pool := testPool(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nested/path", nil)
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /nested/path: got status %d, want %d", rec.Code, http.StatusNotFound)
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
	rec := func(name string) middleware.Middleware {
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

// seedTemplateLink inserts a template link row (target with {N} fragment
// references, optional default location) and schedules its removal. It
// returns the created row for out-of-band inspection.
func seedTemplateLink(t *testing.T, pool *pgxpool.Pool, slug, targetURL, defaultURL string) db.Link {
	t.Helper()

	queries := db.New(pool)
	link, err := queries.CreateLink(context.Background(), db.CreateLinkParams{
		Slug:       slug,
		TargetUrl:  targetURL,
		DefaultUrl: defaultURL,
		CreatedBy:  "server_test",
	})
	if err != nil {
		t.Fatalf("seeding template link %q: %v", slug, err)
	}
	t.Cleanup(func() { _, _ = queries.DeleteLink(context.Background(), link.ID) })
	return link
}

// A template link substitutes {0}, {1}, ... with the path fragments after
// the slug.
func TestTemplateLinkSubstitutesFragments(t *testing.T) {
	pool := testPool(t)
	seedTemplateLink(t, pool, "jira", "https://corp.atlassian.com/issues/{0}-{1}", "https://corp.atlassian.com/board")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jira/board/123", nil)
	New(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("GET /jira/board/123: got status %d, want %d", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://corp.atlassian.com/issues/board-123"; got != want {
		t.Errorf("Location: got %q, want %q", got, want)
	}
}

// Substituted fragments are re-escaped, so decoded characters from the wire
// stay inside one path segment of the destination.
func TestTemplateLinkEscapesFragments(t *testing.T) {
	pool := testPool(t)
	seedTemplateLink(t, pool, "esc", "https://example.com/p/{0}", "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/esc/x%20y", nil)
	New(pool).ServeHTTP(rec, req)

	if got, want := rec.Header().Get("Location"), "https://example.com/p/x%20y"; got != want {
		t.Errorf("Location: got %q, want %q", got, want)
	}
}

// A template link with a default location redirects the bare slug (and the
// trailing-slash form) to the default.
func TestTemplateLinkDefaultLocation(t *testing.T) {
	pool := testPool(t)
	seedTemplateLink(t, pool, "jira", "https://corp.atlassian.com/issues/{0}", "https://corp.atlassian.com/board")

	for _, path := range []string{"/jira", "/jira/"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			New(pool).ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("GET %s: got status %d, want %d", path, rec.Code, http.StatusFound)
			}
			if got, want := rec.Header().Get("Location"), "https://corp.atlassian.com/board"; got != want {
				t.Errorf("Location: got %q, want %q", got, want)
			}
		})
	}
}

// Fragment-count mismatches answer 404 with an explanatory message instead
// of guessing.

func TestTemplateLinkTooFewFragments(t *testing.T) {
	pool := testPool(t)
	seedTemplateLink(t, pool, "jira", "https://corp.atlassian.com/issues/{0}-{1}", "")

	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jira/board", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /jira/board: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), "2 path fragments") {
		t.Errorf("miss page does not explain the fragment count: %q", rec.Body.String())
	}
}

func TestTemplateLinkExtraFragments(t *testing.T) {
	pool := testPool(t)
	seedTemplateLink(t, pool, "jira", "https://corp.atlassian.com/issues/{0}-{1}", "")

	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jira/board/123/extra", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /jira/board/123/extra: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// A template link without a default location 404s on the bare slug.
func TestTemplateLinkBareSlugWithoutDefault(t *testing.T) {
	pool := testPool(t)
	seedTemplateLink(t, pool, "jira", "https://corp.atlassian.com/issues/{0}-{1}", "")

	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jira", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /jira: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// A static link with extra path fragments 404s instead of redirecting with
// the extra path discarded or appended.
func TestStaticLinkRejectsExtraPath(t *testing.T) {
	pool := testPool(t)
	seedLink(t, pool, "wiki", "https://example.com/wiki")

	rec := httptest.NewRecorder()
	New(pool).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wiki/en/typo", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /wiki/en/typo: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), "static link") {
		t.Errorf("miss page does not explain the static slug: %q", rec.Body.String())
	}
}
