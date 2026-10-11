package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/chainguard-dev/clog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/holi0317/golink/db/generated"
	"github.com/holi0317/golink/www"
)

// placeholderCreatedBy stands in for the identity of whoever edits a link
// until tailscale-based identity lands (golink-ux4).
const placeholderCreatedBy = "unknown"

// settingsMux builds the routes of the settings app under /_. It is its own
// mux so routing happens with plain HandleFunc patterns, free of the
// top-level redirect service. Path values are link ids.
//
// Routes:
//   - GET  /_/:            list + create form
//   - POST /_/:            create link
//   - GET  /_/edit/{id}:   edit form
//   - POST /_/edit/{id}:   update link
//   - GET  /_/delete/{id}: delete confirmation
//   - POST /_/delete/{id}: delete link
//   - anything else:       not-found page
func (s *Server) settingsMux() *http.ServeMux {
	settings := http.NewServeMux()
	settings.HandleFunc("GET /{$}", s.listPage)
	settings.HandleFunc("POST /{$}", s.create)
	settings.HandleFunc("GET /edit/{id}", s.editForm)
	settings.HandleFunc("POST /edit/{id}", s.update)
	settings.HandleFunc("GET /delete/{id}", s.deleteConfirm)
	settings.HandleFunc("POST /delete/{id}", s.destroy)
	// Unmatched paths inside the app (unknown page, malformed id) get the
	// not-found page instead of ServeMux's plain one.
	settings.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { notFoundStatus(w, r, "") })
	settings.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) { notFoundStatus(w, r, "") })
	return settings
}

// slugRe matches go-link slugs: non-empty URL unreserved characters only
// (letters, digits, and -._~), so the slug works in a bare path.
var slugRe = regexp.MustCompile(`^[a-zA-Z0-9._~-]+$`)

// validSlug reports whether the slug is usable as a go link. "_" is also
// excluded — it would shadow the settings app at /_.
func validSlug(slug string) bool {
	return slugRe.MatchString(slug) && slug != "_"
}

// validTargetURL reports whether the target is an absolute http(s) URL.
func validTargetURL(raw string) (u *url.URL, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	ok = (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	return u, ok
}

// validateTargetInput checks one create/update submission's target and
// default URLs. Rules:
//   - target must be an absolute http(s) URL;
//   - any "{…}" in the target must be {digits} (positional fragment
//     references, golink-9l6) — messy braces are rejected, not eaten;
//   - default_url is optional, must be http(s), must contain no braces, and
//     only means something when the target is a template.
//
// Returns the collected params plus a user-facing rejection message (empty
// string when valid).
func validateTargetInput(rawTarget, rawDefault string) (targetURL, targetDefault string, rejectMsg string) {
	if _, ok := validTargetURL(rawTarget); !ok {
		return "", "", "target must be an http(s) URL (e.g. https://example.com/hello)"
	}
	if _, ok := parseTemplate(rawTarget); !ok {
		return "", "", "target braces are invalid: template placeholders must be {0}, {1}, … (position after the slug), and stray braces are rejected"
	}
	targetURL, targetDefault = rawTarget, rawDefault
	if rawDefault != "" {
		if _, ok := validTargetURL(rawDefault); !ok {
			return "", "", "default location must be an http(s) URL"
		}
		if maxIdx, ok := parseTemplate(rawTarget); !ok || maxIdx < 0 {
			return "", "", "a default location only applies to template links (targets with {0}, {1}, … placeholders)"
		}
		if strings.ContainsAny(rawDefault, "{}") {
			return "", "", "the default location is a full URL and does not take {0}, {1}, … placeholders"
		}
	}
	return targetURL, targetDefault, ""
}

// listPage renders every stored link with the create form above it.
func (s *Server) listPage(w http.ResponseWriter, r *http.Request) {
	links, err := s.queries.ListLinks(r.Context())
	if err != nil {
		s.fail(w, r, "listing links", err)
		return
	}
	templ.Handler(www.Index(links)).ServeHTTP(w, r)
}

// create stores a new link from the create form and redirects back to the
// list. Duplicate slugs and malformed input answer the error page.
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		s.reject(w, r, "malformed form submission", http.StatusBadRequest)
		return
	}
	slug := r.FormValue("slug")
	targetURL, targetDefault, rejectMsg := validateTargetInput(r.FormValue("target_url"), r.FormValue("default_url"))
	if !validSlug(slug) {
		s.reject(w, r, "slugs may only contain letters, digits, dashes, dots, underscores, and tildes (and may not be “_”)", http.StatusBadRequest)
		return
	}
	if rejectMsg != "" {
		s.reject(w, r, rejectMsg, http.StatusBadRequest)
		return
	}
	if _, err := s.queries.CreateLink(r.Context(), db.CreateLinkParams{
		Slug:       slug,
		TargetUrl:  targetURL,
		DefaultUrl: targetDefault,
		CreatedBy:  placeholderCreatedBy,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			s.reject(w, r, fmt.Sprintf("the go link %q already exists", slug), http.StatusBadRequest)
			return
		}
		log.Errorf("creating link %q: %v", slug, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/_/", http.StatusSeeOther)
}

// editForm renders the edit form for a stored link.
func (s *Server) editForm(w http.ResponseWriter, r *http.Request) {
	link, ok := s.byID(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	templ.Handler(www.Edit(link)).ServeHTTP(w, r)
}

// update records a new target URL for the link and redirects back to the
// list. Rows are addressed by id; the slug is what gets published, so it is
// never an edit handle.
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	link, ok := s.byID(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.reject(w, r, "malformed form submission", http.StatusBadRequest)
		return
	}
	targetURL, targetDefault, rejectMsg := validateTargetInput(r.FormValue("target_url"), r.FormValue("default_url"))
	if rejectMsg != "" {
		s.reject(w, r, rejectMsg, http.StatusBadRequest)
		return
	}
	if _, err := s.queries.UpdateLink(r.Context(), db.UpdateLinkParams{
		ID:         link.ID,
		TargetUrl:  targetURL,
		DefaultUrl: targetDefault,
	}); err != nil {
		log.Errorf("updating link %d: %v", link.ID, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/_/", http.StatusSeeOther)
}

// deleteConfirm renders the irreversible-deletion warning.
func (s *Server) deleteConfirm(w http.ResponseWriter, r *http.Request) {
	link, ok := s.byID(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	templ.Handler(www.DeleteConfirm(link)).ServeHTTP(w, r)
}

// destroy removes the link row and redirects back to the list.
func (s *Server) destroy(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	link, ok := s.byID(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if _, err := s.queries.DeleteLink(r.Context(), link.ID); err != nil {
		log.Errorf("deleting link %d: %v", link.ID, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/_/", http.StatusSeeOther)
}

// byID fetches one link for the settings app. A miss — unknown or malformed
// id — renders the not-found page and returns false, so callers can stop;
// other storage errors answer 500 and also return false.
func (s *Server) byID(w http.ResponseWriter, r *http.Request, rawID string) (db.Link, bool) {
	log := clog.FromContext(r.Context())
	id, err := strconv.ParseInt(rawID, 10, 32)
	if err != nil {
		notFoundStatus(w, r, "")
		return db.Link{}, false
	}
	link, err := s.queries.GetLinkByID(r.Context(), int32(id))
	if errors.Is(err, pgx.ErrNoRows) {
		notFoundStatus(w, r, "")
		return db.Link{}, false
	}
	if err != nil {
		log.Errorf("resolving link id %q: %v", rawID, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return db.Link{}, false
	}
	return link, true
}

// fail logs a storage failure and answers a plain 500.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	clog.FromContext(r.Context()).Errorf("%s: %v", what, err)
	http.Error(w, "golink: storage error", http.StatusInternalServerError)
}

// reject answers the error page with a user-facing message and status.
func (s *Server) reject(w http.ResponseWriter, r *http.Request, message string, status int) {
	templ.Handler(www.Error(message), templ.WithStatus(status)).ServeHTTP(w, r)
}

// notFoundStatus renders the not-found page. An empty slug stands for "no
// slug here" (unknown page, malformed id).
func notFoundStatus(w http.ResponseWriter, r *http.Request, slug string) {
	templ.Handler(www.NotFound(slug), templ.WithStatus(http.StatusNotFound)).ServeHTTP(w, r)
}
