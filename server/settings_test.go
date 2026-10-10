package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

// deleteRow removes the link row after the test, keyed by id.
func deleteRow(pool *pgxpool.Pool, id int32) func() {
	return func() { _, _ = db.New(pool).DeleteLink(context.Background(), id) }
}

// The list page shows every stored link with its edit and delete actions.
func TestSettingsListShowsLinks(t *testing.T) {
	pool := testPool(t)
	first := seedLink(t, pool, "greet", "https://example.com/hello")
	second := seedLink(t, pool, "issue", "https://example.com/issues")

	rec := settingsGET(t, pool, "/_/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /_/: got status %d, want %d", rec.Code, http.StatusOK)
	}
	for _, fragment := range []string{
		"greet",
		"issue",
		"https://example.com/hello",
		"/_/edit/" + strconv.Itoa(int(first.ID)),
		"/_/delete/" + strconv.Itoa(int(second.ID)),
	} {
		if !strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("list page: body misses %q", fragment)
		}
	}
}

// Posting a valid create form stores the link and redirects back to the
// list; the link is then live as a go link.
func TestSettingsCreateLinks(t *testing.T) {
	pool := testPool(t)

	rec := settingsPOST(t, pool, "/_/", url.Values{
		"slug":       {"fresh"},
		"target_url": {"https://example.com/new"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /_/: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got, want := rec.Header().Get("Location"), "/_/"; got != want {
		t.Errorf("POST /_/ Location: got %q, want %q", got, want)
	}

	created, err := db.New(pool).GetLink(context.Background(), "fresh")
	if err != nil {
		t.Fatalf("created link missing from database: %v", err)
	}
	t.Cleanup(deleteRow(pool, created.ID))
	if created.TargetUrl != "https://example.com/new" {
		t.Errorf("target_url: got %q, want %q", created.TargetUrl, "https://example.com/new")
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
		{name: "underscores-only shadow slug", fields: url.Values{
			"slug": {"_"}, "target_url": {"https://example.com/x"},
		}, wantMessage: "slugs"},
		{name: "non-http(s) target", fields: url.Values{
			"slug": {"cute"}, "target_url": {"ftp://example.com/file"},
		}, wantMessage: "http(s)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := settingsPOST(t, pool, "/_/", c.fields)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST /_/: got status %d, want %d", rec.Code, http.StatusBadRequest)
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
		t.Errorf("POST /_/: got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "already exists") {
		t.Errorf("duplicate page: body misses the duplicate message: %q", rec.Body.String())
	}
}

// The edit form prefills the stored target; updating it rewrites the row
// while the slug stays untouched. Edits address the row by id, not slug.
func TestSettingsEditUpdatesLink(t *testing.T) {
	pool := testPool(t)
	seeded := seedLink(t, pool, "store", "https://example.com/shop")
	editPath := "/_/edit/" + strconv.Itoa(int(seeded.ID))

	rec := settingsPOST(t, pool, editPath, url.Values{"target_url": {"https://example.com/market"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: got status %d, want %d", editPath, rec.Code, http.StatusSeeOther)
	}

	link, err := db.New(pool).GetLink(context.Background(), "store")
	if err != nil {
		t.Fatalf("updated link missing from database: %v", err)
	}
	if link.TargetUrl != "https://example.com/market" {
		t.Errorf("target_url: got %q, want %q", link.TargetUrl, "https://example.com/market")
	}
	if link.ID != seeded.ID {
		t.Errorf("edit address by id: row id changed from %d to %d", seeded.ID, link.ID)
	}
}

// Editing an unknown or malformed id answers the not-found page.
func TestSettingsEditUnknownID(t *testing.T) {
	pool := testPool(t) // no rows seeded

	for _, path := range []string{"/_/edit/99999", "/_/edit/abc"} {
		rec := settingsGET(t, pool, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: got status %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}
}

// The delete confirmation page shows the link; posting removes it, and the
// slug stops resolving.
func TestSettingsDeleteRemovesLink(t *testing.T) {
	pool := testPool(t)
	seeded := seedLink(t, pool, "chore", "https://example.com/chore")
	deletePath := "/_/delete/" + strconv.Itoa(int(seeded.ID))

	rec := settingsGET(t, pool, deletePath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: got status %d, want %d", deletePath, rec.Code, http.StatusOK)
	}

	rec = settingsPOST(t, pool, deletePath, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: got status %d, want %d", deletePath, rec.Code, http.StatusSeeOther)
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
