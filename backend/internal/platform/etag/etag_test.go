package etag

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

func TestParse(t *testing.T) {
	for header, want := range map[string]map[int64]bool{
		"":                      {1: true, 9: true},
		"  ":                    {1: true},
		"*":                     {1: true, 9: true},
		`"7"`:                   {7: true, 8: false},
		` "7" , "9" `:           {7: true, 9: true, 8: false},
		`W/"7"`:                 {7: false},
		`W/"7", "8"`:            {7: false, 8: true},
		`"abc"`:                 {1: false},
		`""`:                    {1: false},
		`"7",` + ` "07"`:        {7: true},
		`"9223372036854775807"`: {9223372036854775807: true},
	} {
		m, err := Parse(header)
		require.NoError(t, err, header)
		for v, ok := range want {
			assert.Equal(t, ok, m.Matches(v), "%q matches %d", header, v)
		}
		assert.Equal(t, header != "" && header != "  ", m.Present(), header)
	}
	for _, bad := range []string{"7", `"7`, `'7'`, `"7" "8"`, `"7",`, `*, "7"`, `w/"7"`, "\"a\x00\"", `"a b"`, `"é"`} {
		_, err := Parse(bad)
		e, ok := apperr.As(err)
		require.True(t, ok, bad)
		assert.Equal(t, "If-Match", e.Fields[0].Field, bad)
	}
}

func TestFromRequest(t *testing.T) {
	r := httptest.NewRequest("PATCH", "/", nil)
	m, err := FromRequest(r)
	require.NoError(t, err)
	assert.False(t, m.Present())
	r.Header.Add("If-Match", `"3"`)
	r.Header.Add("If-Match", `"5"`)
	m, err = FromRequest(r)
	require.NoError(t, err)
	assert.True(t, m.Matches(3))
	assert.True(t, m.Matches(5), "several header lines form one list")
	assert.Equal(t, `"42"`, Tag(42))
}
