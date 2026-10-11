// Package redirect implements the core golink engine: resolving slugs from
// storage and answering redirects, including template links with {N}
// positional fragment references.
package redirect

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/jackc/pgx/v5"

	"github.com/holi0317/golink/db/generated"
)

// Handler carries the dependencies the redirect handlers need. More
// dependencies are a new field plus constructor wiring, and tests can build
// a Handler directly.
type Handler struct {
	Queries *db.Queries
}

// ServeHTTP resolves the request slug and answers 302 with the URL stored in
// the database. A miss renders the not-found page; any other storage error
// is logged and answers 500.
//
// The same handler serves both `GET /{slug}` (no fragments) and
// `GET /{slug}/{rest...}`: PathValue("rest") is empty on the former. A
// static link ignores nothing — fragments after it are a 404 — while a
// template link substitutes {0}, {1}, ... with the fragments of {rest...},
// falling back to its default_url when no fragments are given.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	slug := r.PathValue("slug")
	link, err := h.Queries.GetLink(r.Context(), slug)
	if errors.Is(err, pgx.ErrNoRows) {
		h.NotFound(slug)(w, r)
		return
	}
	if err != nil {
		log.Errorf("resolving slug %q: %v", slug, err)
		http.Error(w, "golink: storage error", http.StatusInternalServerError)
		return
	}

	fragments := []string{}
	if rest := r.PathValue("rest"); rest != "" {
		fragments = strings.Split(rest, "/")
	}
	target, ok := ExpandTarget(link, fragments)
	if !ok {
		h.reject(w, r, fragmentMissMessage(link, fragments), http.StatusNotFound)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// NotFound is the miss page: it asks whether the visitor wants to create the
// go link. An empty slug stands for paths with no slug at all (root,
// nested).
func (h *Handler) NotFound(slug string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderNotFound(w, r, slug)
	}
}

// reject answers the error page with a user-facing message and status.
func (h *Handler) reject(w http.ResponseWriter, r *http.Request, message string, status int) {
	renderError(w, r, message, status)
}

// fragmentMissMessage explains why a template or static link refused the
// request's path, for the 404 page.
func fragmentMissMessage(link db.Link, fragments []string) string {
	if IsTemplate(link.TargetUrl) {
		if maxIdx, ok := ParseTemplate(link.TargetUrl); ok && len(fragments) > 0 {
			return fmt.Sprintf("%s needs %d path fragments here, got %d",
				link.Slug, maxIdx+1, len(fragments))
		}
		return fmt.Sprintf("%s is a template link: it expects path fragments "+
			"after the slug%s", link.Slug, defaultHint(link))
	}
	return fmt.Sprintf("%s is a static link and takes no path fragments", link.Slug)
}

// defaultHint mentions the fallback location when a template link has one.
func defaultHint(link db.Link) string {
	if link.DefaultUrl != "" {
		return "; the bare slug redirects to its default location"
	}
	return ""
}
