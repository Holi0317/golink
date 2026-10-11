// Package server implements the golink HTTP surface: the top-level mux and
// its handlers.
package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holi0317/golink/db/generated"
)

// Middleware wraps an http.Handler with cross-cutting behavior.
type Middleware = func(http.Handler) http.Handler

// Server carries the dependencies the HTTP handlers need. Handlers are
// methods on it, so more dependencies (templ components and the like) are a
// new field plus constructor wiring, and tests can build a Server directly.
type Server struct {
	queries *db.Queries
}

// New returns the top-level HTTP handler for the golink service.
//
// The connection pool is the first parameter as a DI seam: it feeds the
// Server's dependencies instead of reaching for globals. The middlewares are
// composed in reverse application order: the first middleware sits outermost
// and sees every request first.
//
// Routes:
//   - GET /: redirect to the settings app at /_.
//   - GET /{slug} and GET /{slug}/{rest...}: core redirect service; 302 to
//     the URL stored in the database, not-found page on a miss. Template
//     links (golink-9l6) substitute {0}, {1}, ... references with the path
//     fragments in {rest...}.
//   - /_/*: the settings app (golink-71w for the templ UI).
func New(pool *pgxpool.Pool, mws ...Middleware) http.Handler {
	s := &Server{queries: db.New(pool)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// The settings app is the front door; / has no meaning other than
		// pointing people there.
		http.Redirect(w, r, "/_/", http.StatusTemporaryRedirect)
	})
	// The settings app is its own mux rooted at /_, mounted with a two-way
	// StripPrefix so its handlers route with plain patterns: "/_/" maps to
	// "/" and "/_/edit/42" to "/edit/42". The bare "/_" is an explicit
	// redirect because the wildcard above does not cover it reliably.
	settings := http.StripPrefix("/_", s.settingsMux())
	redebounce := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/_/", http.StatusTemporaryRedirect)
	}
	mux.HandleFunc("GET /_", redebounce)
	mux.HandleFunc("POST /_", redebounce)
	mux.Handle("GET /_/{path...}", settings)
	mux.Handle("POST /_/{path...}", settings)
	mux.HandleFunc("GET /{slug}", s.redirect)
	// Longer paths hit a template link's fragment route; ServeMux resolves
	// the two patterns above by specificity, so single-segment requests
	// still take /{slug}.
	mux.HandleFunc("GET /{slug}/{rest...}", s.redirect)
	// With both redirect routes covered, this catch-all only serves "/"
	// itself ({$} handles "/" too, so this is effectively unreachable; kept
	// as the not-found floor for paths like "//" and malformed ones).
	mux.HandleFunc("GET /", s.notFound(""))

	var h http.Handler = mux
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// Handlers below this line carry the core redirect logic. Template helpers
// are shared with the settings app, which validates targets on save.

// anyTokenRe matches a {N} reference anywhere in a template, for
// substitution and template detection.
var anyTokenRe = regexp.MustCompile(`\{(\d+)\}`)

// tokenRe is anyTokenRe anchored at the start of a string: parseTemplate
// scans suffixes of the target, and only a token opening exactly at the
// scan position is valid.
var tokenRe = regexp.MustCompile(`^\{(\d+)\}`)

// isTemplate reports whether a target is a valid template, i.e. references
// at least one {N} fragment and has no invalid braces.
func isTemplate(target string) bool {
	maxIdx, ok := parseTemplate(target)
	return ok && maxIdx >= 0
}

// parseTemplate validates a target's braces and reports the highest
// fragment index it references (-1 for static). Scanning is byte-wise and
// strict: every "{" must open a {digits} token and every "}" must close
// one, so a typo like "{1" or "{}" cannot be stored and silently
// substitute the empty string at serve time. Marked unexported for use by
// the settings validation as well.
func parseTemplate(target string) (maxIdx int, ok bool) {
	maxIdx = -1
	for i := 0; i < len(target); {
		switch target[i] {
		case '{':
			m := tokenRe.FindStringSubmatch(target[i:])
			if m == nil {
				return 0, false
			}
			idx, _ := strconv.Atoi(m[1])
			maxIdx = max(maxIdx, idx)
			// Bound the index: a stored-out-of-band template must never
			// push the fragment-count check into absurd territory.
			if idx > 1024 {
				return 0, false
			}
			i += len(m[0])
		case '}':
			// Stray closer: matchless brace.
			return 0, false
		default:
			i++
		}
	}
	return maxIdx, true
}

// redirect resolves the request slug and answers 302 with the URL stored in
// the database. A miss renders the not-found page; any other storage error
// is logged and answers 500.
//
// The same handler serves both `GET /{slug}` (no fragments) and
// `GET /{slug}/{rest...}`: PathValue("rest") is empty on the former. A
// static link ignores nothing — fragments after it are a 404 — while a
// template link substitutes {0}, {1}, ... with the fragments of {rest...},
// falling back to its default_url when no fragments are given.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	slug := r.PathValue("slug")
	link, err := s.queries.GetLink(r.Context(), slug)
	if errors.Is(err, pgx.ErrNoRows) {
		s.notFound(slug)(w, r)
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
	target, ok := expandTarget(link, fragments)
	if !ok {
		s.reject(w, r, fragmentMissMessage(link, fragments), http.StatusNotFound)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// expandTarget resolves a link row for a request with the given path
// fragments (after the slug). Static targets come back untouched — callers
// treat fragments on a static link as a miss. Template targets substitute
// every {N} with fragment[N], re-escaped with url.PathEscape: PathValue
// hands back decoded segments, and re-escaping keeps a decoded "/" inside a
// fragment (a %2F from the wire) a single segment in the destination. The
// second return value is false when the fragments cannot fill the template
// or fall back to a default.
func expandTarget(link db.Link, fragments []string) (string, bool) {
	maxIdx, ok := parseTemplate(link.TargetUrl)
	if !ok {
		// Corrupt template stored out-of-band; treat like a fragment miss.
		return "", false
	}
	if maxIdx < 0 {
		// Static target: any fragments after the slug are a miss.
		return link.TargetUrl, len(fragments) == 0
	}
	if len(fragments) == 0 {
		if link.DefaultUrl != "" {
			return link.DefaultUrl, true
		}
		return "", false
	}
	if len(fragments) != maxIdx+1 {
		return "", false
	}
	return anyTokenRe.ReplaceAllStringFunc(link.TargetUrl, func(tok string) string {
		idx, _ := strconv.Atoi(anyTokenRe.FindStringSubmatch(tok)[1])
		return url.PathEscape(fragments[idx])
	}), true
}

// fragmentMissMessage explains why a template or static link refused the
// request's path, for the 404 page.
func fragmentMissMessage(link db.Link, fragments []string) string {
	if isTemplate(link.TargetUrl) {
		if maxIdx, ok := parseTemplate(link.TargetUrl); ok && len(fragments) > 0 {
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

// notFound is the miss page: it asks whether the visitor wants to create the
// go link. An empty slug stands for paths with no slug at all (root,
// nested).
func (s *Server) notFound(slug string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notFoundStatus(w, r, slug)
	}
}
