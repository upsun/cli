package mockapi

import (
	"errors"
	"net/http"
	"strconv"
)

// paginate returns a page of items, and adds a "next" link to links if there are more. Like the API, it reads the
// page size from "page[size]" (up to maxSize, which is also the default), and the position from an opaque
// "page[after]" cursor. It writes an error and returns false if either is invalid.
func paginate[T any](w http.ResponseWriter, req *http.Request, items []T, maxSize int, links HalLinks) ([]T, bool) {
	query := req.URL.Query()
	size := maxSize
	if s := query.Get("page[size]"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxSize {
			writeProblem(w, http.StatusBadRequest, errors.New("invalid page size"))
			return nil, false
		}
		size = n
	}
	offset := 0
	if c := query.Get("page[after]"); c != "" {
		s, ok := decodeCursor(c)
		n, err := strconv.Atoi(s)
		if !ok || err != nil || n < 0 {
			writeProblem(w, http.StatusBadRequest, errors.New("invalid cursor"))
			return nil, false
		}
		offset = min(n, len(items))
	}
	end := min(offset+size, len(items))
	if end < len(items) {
		query.Set("page[after]", encodeCursor(strconv.Itoa(end)))
		links["next"] = HALLink{HREF: req.URL.Path + "?" + query.Encode()}
	}
	return items[offset:end], true
}
