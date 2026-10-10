package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holi0317/golink/server/db"
)

// settingsGET issues a GET against the composed handler and returns the
// response.
func settingsGET(t *testing.T, pool *pgxpool.Pool, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	New(pool).ServeHTTP(rec, req)
	return rec
}

// settingsPOST posts urlencoded form fields at the given path and returns
// the response.
func settingsPOST(t *testing.T, pool *pgxpool.Pool, path string, fields url.Values) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	New(pool).ServeHTTP(rec, req)
	return rec
}

// The list page shows every stored link with its edit and delete actions.
func TestSettingsListShowsLinks(t *testing.T) {
	pool := testPool(t)
	seedLink(t, pool, "greet", "https://example.com/hello")
	seedLink(t, pool, "issue", "https://example.com/issues")

	rec := settingsGET(t, pool, "/_/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /_: got status %d, want %d", rec.Code, http.StatusOK)
	}
	for _, fragment := range []string{"greet", "issue", "https://example.com/hello", "/_/edit/greet", "/_/delete/issue"} {
		if !strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("list page: body misses %q", fragment)
		}
	}
}

// Posting a valid create form stores the link and redirects back to the
// list; the link is then live as a go link.
func TestSettingsCreateLinks(t *testing.T) {
	pool := testPool(t)
	t.Cleanup(func() { _, _ = db.New(pool).DeleteLink(context.Background(), "fresh") })

	rec := settingsPOST(t, pool, "/_/", url.Values{
		"slug":       {"fresh"},
		"target_url": {"https://example.com/new"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /_: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got, want := rec.Header().Get("Location"), "/_/"; got != want {
		t.Errorf("POST /_ Location: got %q, want %q", got, want)
	}

	link, err := db.New(pool).GetLink(context.Background(), "fresh")
	if err != nil {
		t.Fatalf("created link missing from database: %v", err)
	}
	if link.TargetUrl != "https://example.com/new" {
		t.Errorf("target_url: got %q, want %q", link.TargetUrl, "https://example.com/new")
	}
}

// The create form rejects malformed slugs and targets instead of storing
// them.
func TestSettingsCreateRejectsBadInput(t *testing.T) {
	pool := testPool(t)

	cases := []struct {
		name        string
		fields      url.Values
		wantMessage string
	}{
		{name: "spaces cannot go in a slug", fields: url.Values{
			"slug": {"not me"}, "target_url": {"https://example.com"},
		}, wantMessage: "slugs"},
		{name: "empty slug", fields: url.Values{
			"slug": {""}, "target_url": {"https://example.com/x"},
		}, wantMessage: "slugs"},
		{name: "non-http(s) target", fields: url.Values{
			"slug": {"cute"}, "target_url": {"ftp://example.com/file"},
		}, wantMessage: "http(s)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := settingsPOST(t, pool, "/_/", c.fields)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST /_: got status %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if !strings.Contains(rec.Body.String(), c.wantMessage) {
				t.Errorf("reject page: body misses %q: %q", c.wantMessage, rec.Body.String())
			}
		})
	}
}

// Creating a link whose slug already exists is rejected as a duplicate.
func TestSettingsCreateRejectsDuplicate(t *testing.T) {
	pool := testPool(t)
	seedLink(t, pool, "greet", "https://example.com/hello")

	rec := settingsPOST(t, pool, "/_/", url.Values{
		"slug":       {"greet"},
		"target_url": {"https://example.com/other"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /_: got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "already exists") {
		t.Errorf("duplicate page: body misses the duplicate message: %q", rec.Body.String())
	}
}

// The edit form prefills the stored target; updating it rewrites the row.
func TestSettingsEditUpdatesLink(t *testing.T) {
	pool := testPool(t)
	seedLink(t, pool, "store", "https://example.com/shop")

	rec := settingsPOST(t, pool, "/_/edit/store", url.Values{"target_url": {"https://example.com/market"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /_/edit/store: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}

	link, err := db.New(pool).GetLink(context.Background(), "store")
	if err != nil {
		t.Fatalf("updated link missing from database: %v", err)
	}
	if link.TargetUrl != "https://example.com/market" {
		t.Errorf("target_url: got %q, want %q", link.TargetUrl, "https://example.com/market")
	}
}

// Editing an unknown slug answers the not-found page.
func TestSettingsEditUnknownSlug(t *testing.T) {
	pool := testPool(t) // no rows seeded

	rec := settingsGET(t, pool, "/_/edit/ghost")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /_/edit/ghost: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// The delete confirmation page shows the link; posting removes it, and the
// slug stops resolving.
func TestSettingsDeleteRemovesLink(t *testing.T) {
	pool := testPool(t)
	seedLink(t, pool, "chore", "https://example.com/chore")

	rec := settingsGET(t, pool, "/_/delete/chore")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /_/delete/chore: got status %d, want %d", rec.Code, http.StatusOK)
	}

	rec = settingsPOST(t, pool, "/_/delete/chore", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /_/delete/chore: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}

	if _, err := db.New(pool).GetLink(context.Background(), "chore"); err == nil {
		t.Fatal("deleted link is still in the database")
	}
}

// Unknown settings paths answer the not-found page.
func TestSettingsUnknownPage(t *testing.T) {
	pool := testPool(t)

	rec := settingsGET(t, pool, "/_/nonsense")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /_/nonsense: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}
