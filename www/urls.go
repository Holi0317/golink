package www

import (
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
