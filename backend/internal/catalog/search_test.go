package catalog

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ?q= searches a title (wildcards escaped) or a key or number (DEC-78).
func TestSearchQuery(t *testing.T) {
	read := func(q string) (ListFilter, error) {
		return searchQuery(httptest.NewRequest("GET", "/api/v1/test-cases?q="+url.QueryEscape(q), nil), ListFilter{})
	}
	f, err := searchQuery(httptest.NewRequest("GET", "/api/v1/test-cases", nil), ListFilter{})
	require.NoError(t, err)
	assert.Nil(t, f.Search)

	f, err = read(" Pay 50%_off\\ ")
	require.NoError(t, err)
	assert.Equal(t, `Pay 50\%\_off\\`, *f.Search)
	assert.Nil(t, f.SearchNumber)
	for q, n := range map[string]int64{"CHK-12": 12, "chk-12": 12, "12": 12} {
		f, err = read(q)
		require.NoError(t, err)
		assert.Equal(t, n, *f.SearchNumber, q)
		assert.Equal(t, q, *f.Search, "the title may contain it too")
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 201), "a\x00b"} {
		_, err = read(bad)
		assert.Error(t, err, "%q", bad)
	}
}
