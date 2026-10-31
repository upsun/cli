package mockapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaginate(t *testing.T) {
	items := []string{"a", "b", "c"}

	// get paginates the items for a request URL, and returns the page and the next link.
	get := func(target string) (page []string, next string, status int) {
		w := httptest.NewRecorder()
		links := HalLinks{}
		page, ok := paginate(w, httptest.NewRequest(http.MethodGet, target, http.NoBody), items, 2, links)
		if !ok {
			return nil, "", w.Code
		}
		return page, links["next"].HREF, http.StatusOK
	}

	page, next, _ := get("/items")
	assert.Equal(t, []string{"a", "b"}, page)
	require.NotEmpty(t, next)

	page, next, _ = get(next)
	assert.Equal(t, []string{"c"}, page)
	assert.Empty(t, next)

	page, next, _ = get("/items?page[size]=1&sort=x")
	assert.Equal(t, []string{"a"}, page)
	u, err := url.Parse(next)
	require.NoError(t, err)
	assert.Equal(t, "/items", u.Path)
	assert.Equal(t, "x", u.Query().Get("sort"))

	for _, q := range []string{"page[size]=0", "page[size]=3", "page[after]=x"} {
		_, _, status := get("/items?" + q)
		assert.Equal(t, http.StatusBadRequest, status, q)
	}
}
