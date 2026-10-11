package redirect

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/a-h/templ"

	"github.com/holi0317/golink/db/generated"
	"github.com/holi0317/golink/www"
)

// anyTokenRe matches a {N} reference anywhere in a template, for
// substitution and template detection.
var anyTokenRe = regexp.MustCompile(`\{(\d+)\}`)

// tokenRe is anyTokenRe anchored at the start of a string: ParseTemplate
// scans suffixes of the target, and only a token opening exactly at the
// scan position is valid.
var tokenRe = regexp.MustCompile(`^\{(\d+)\}`)

// IsTemplate reports whether a target is a valid template, i.e. references
// at least one {N} fragment and has no invalid braces.
func IsTemplate(target string) bool {
	maxIdx, ok := ParseTemplate(target)
	return ok && maxIdx >= 0
}

// ParseTemplate validates a target's braces and reports the highest
// fragment index it references (-1 for static). Scanning is byte-wise and
// strict: every "{" must open a {digits} token and every "}" must close
// one, so a typo like "{1" or "{}" cannot be stored and silently
// substitute the empty string at serve time.
func ParseTemplate(target string) (maxIdx int, ok bool) {
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

// ExpandTarget resolves a link row for a request with the given path
// fragments (after the slug). Static targets come back untouched — callers
// treat fragments on a static link as a miss. Template targets substitute
// every {N} with fragment[N], re-escaped with url.PathEscape: PathValue
// hands back decoded segments, and re-escaping keeps a decoded "/" inside a
// fragment (a %2F from the wire) a single segment in the destination. The
// second return value is false when the fragments cannot fill the template
// or fall back to a default.
func ExpandTarget(link db.Link, fragments []string) (string, bool) {
	maxIdx, ok := ParseTemplate(link.TargetUrl)
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

// renderError answers the error page with a user-facing message and status.
func renderError(w http.ResponseWriter, r *http.Request, message string, status int) {
	templ.Handler(www.Error(message), templ.WithStatus(status)).ServeHTTP(w, r)
}

// renderNotFound renders the not-found page. An empty slug stands for "no
// slug here" (unknown page, malformed id).
func renderNotFound(w http.ResponseWriter, r *http.Request, slug string) {
	templ.Handler(www.NotFound(slug), templ.WithStatus(http.StatusNotFound)).ServeHTTP(w, r)
}
