package www

import (
	"net/url"
	"strconv"

	"github.com/a-h/templ"
)

// editURL and deleteURL address a link by its id: the slug gets published,
// so it must not double as an edit handle.
func editURL(id int32) templ.SafeURL {
	return templ.URL("/_/edit/" + strconv.FormatInt(int64(id), 10))
}

func deleteURL(id int32) templ.SafeURL {
	return templ.URL("/_/delete/" + strconv.FormatInt(int64(id), 10))
}

// createURL builds the settings list page with an optional pre-filled slug
// for the create form, e.g. /_/?slug=greet. An empty slug omits the query.
// The value is percent-encoded so arbitrary slugs (and fragments after a
// template-link miss) survive the round-trip.
func createURL(slug string) templ.SafeURL {
	if slug == "" {
		return templ.URL("/_/")
	}
	return templ.URL("/_/?slug=" + url.QueryEscape(slug))
}

// templateHint explains {0}, {1}, ... fragment references for template
// targets. Held as a constant because templ's parser chokes on bare braces
// in markup.
const templateHint = "Use {0}, {1}, … in the target to reference path fragments after the slug — e.g. https://corp.atlassian.com/issues/{0}-{1} for go/jira/board/123."

// defaultHint explains the optional default location field.
const defaultHint = "Optional. Where the bare slug (just go/<slug>, no fragments) redirects for template links; leave empty to 404 on the bare slug."
