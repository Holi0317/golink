package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/chainguard-dev/clog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/holi0317/golink/server/db"
	"github.com/holi0317/golink/www"
)

// placeholderCreatedBy stands in for the identity of whoever edits a link
// until tailscale-based identity lands (golink-ux4).
const placeholderCreatedBy = "unknown"

// validSlug reports whether the slug is usable as a go link: non-empty URL
// unreserved characters only (letters, digits, and -._~), so the slug works
// in a bare path. "_" is also excluded — it would shadow the settings app
// at /_.
func validSlug(slug string) bool {
	if slug == "" || slug == "_" {
		return false
	}
	for _, c := range slug {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' || c == '.' || c == '_' || c == '~':
		default:
			return false
		}
	}
	return true
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

// settings serves the whole settings app under /_: the patterns
// "GET /_/{path...}" and "POST /_/{path...}" both land here, and it routes
// on the remaining path and method. Unknown paths answer the not-found page.
//
// Routes:
//   - GET /_             list + create form
//   - POST /_            create link
//   - GET /_/edit/{s}    edit form
//   - POST /_/edit/{s}   update link
//   - GET /_/delete/{s}  delete confirmation
//   - POST /_/delete/{s} delete link
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	head, tail, _ := strings.Cut(r.PathValue("path"), "/")

	switch {
	case head == "": // "/_"
		s.postOrGet(w, r, s.create, s.listPage)

	case head == "edit" && tail != "": // "edit/{slug}"
		s.postOrGet(w, r, func(w http.ResponseWriter, r *http.Request) { s.update(w, r, tail) }, func(w http.ResponseWriter, r *http.Request) { s.editForm(w, r, tail) })

	case head == "delete" && tail != "": // "delete/{slug}"
		s.postOrGet(w, r, func(w http.ResponseWriter, r *http.Request) { s.destroy(w, r, tail) }, func(w http.ResponseWriter, r *http.Request) { s.deleteConfirm(w, r, tail) })

	default:
		notFoundStatus(w, r, "")
	}
}

// postOrGet dispatches between the POST and GET handlers for a settings
// route; anything else answers 405 with an Allow header.
func (s *Server) postOrGet(w http.ResponseWriter, r *http.Request, post, get http.HandlerFunc) {
	switch r.Method {
	case http.MethodPost:
		post(w, r)
	case http.MethodGet:
		get(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
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
	rawURL := r.FormValue("target_url")
	if !validSlug(slug) {
		s.reject(w, r, "slugs may only contain letters, digits, dashes, dots, underscores, and tildes (and may not be “_”)", http.StatusBadRequest)
		return
	}
	if _, ok := validTargetURL(rawURL); !ok {
		s.reject(w, r, "target must be an http(s) URL (e.g. https://example.com/hello)", http.StatusBadRequest)
		return
	}
	if _, err := s.queries.CreateLink(r.Context(), db.CreateLinkParams{
		Slug:      slug,
		TargetUrl: rawURL,
		CreatedBy: placeholderCreatedBy,
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
func (s *Server) editForm(w http.ResponseWriter, r *http.Request, slug string) {
	link, ok := s.bySlug(w, r, slug)
	if !ok {
		return
	}
	templ.Handler(www.Edit(link)).ServeHTTP(w, r)
}

// update records a new target URL for the slug and redirects back to the
// list. The slug itself is fixed: renaming would break published links;
// delete and recreate instead.
func (s *Server) update(w http.ResponseWriter, r *http.Request, slug string) {
	log := clog.FromContext(r.Context())
	if _, ok := s.bySlug(w, r, slug); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.reject(w, r, "malformed form submission", http.StatusBadRequest)
		return
	}
	rawURL := r.FormValue("target_url")
	if _, ok := validTargetURL(rawURL); !ok {
		s.reject(w, r, "target must be an http(s) URL (e.g. https://example.com/hello)", http.StatusBadRequest)
		return
	}
	if _, err := s.queries.UpdateLink(r.Context(), db.UpdateLinkParams{
		Slug:      slug,
		TargetUrl: rawURL,
	}); err != nil {
		log.Errorf("updating link %q: %v", slug, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/_/", http.StatusSeeOther)
}

// deleteConfirm renders the irreversible-deletion warning.
func (s *Server) deleteConfirm(w http.ResponseWriter, r *http.Request, slug string) {
	link, ok := s.bySlug(w, r, slug)
	if !ok {
		return
	}
	templ.Handler(www.DeleteConfirm(link)).ServeHTTP(w, r)
}

// destroy removes the link row and redirects back to the list.
func (s *Server) destroy(w http.ResponseWriter, r *http.Request, slug string) {
	log := clog.FromContext(r.Context())
	if _, ok := s.bySlug(w, r, slug); !ok {
		return
	}
	if _, err := s.queries.DeleteLink(r.Context(), slug); err != nil {
		log.Errorf("deleting link %q: %v", slug, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/_/", http.StatusSeeOther)
}

// bySlug fetches one link for the settings app. A miss renders the
// not-found page and returns false, so callers can stop; other storage
// errors answer 500 and also return false.
func (s *Server) bySlug(w http.ResponseWriter, r *http.Request, slug string) (db.Link, bool) {
	log := clog.FromContext(r.Context())
	link, err := s.queries.GetLink(r.Context(), slug)
	if errors.Is(err, pgx.ErrNoRows) {
		notFoundStatus(w, r, slug)
		return db.Link{}, false
	}
	if err != nil {
		log.Errorf("resolving slug %q: %v", slug, err)
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
// slug here" (root, nested paths).
func notFoundStatus(w http.ResponseWriter, r *http.Request, slug string) {
	templ.Handler(www.NotFound(slug), templ.WithStatus(http.StatusNotFound)).ServeHTTP(w, r)
}
