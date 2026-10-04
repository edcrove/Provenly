package pagination

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPage(t *testing.T) {
	p := Default()
	assert.Equal(t, Page{Number: 1, Size: DefaultSize}, p)
	p = Page{Number: 3, Size: 10}
	assert.Equal(t, int32(10), p.Limit())
	assert.Equal(t, int32(20), p.Offset())
}

func TestResultTotalPagesAndMap(t *testing.T) {
	cases := []struct {
		total int64
		pages int32
	}{{0, 0}, {1, 1}, {10, 1}, {11, 2}}
	for _, c := range cases {
		r := Result[int]{Page: Page{Number: 1, Size: 10}, Total: c.total}
		assert.Equal(t, c.pages, r.TotalPages(), "total=%d", c.total)
	}
	r := Result[int]{Items: []int{1, 2}, Page: Page{Number: 2, Size: 2}, Total: 4}
	m := Map(r, strconv.Itoa)
	assert.Equal(t, []string{"1", "2"}, m.Items)
	assert.Equal(t, r.Page, m.Page)
	assert.Equal(t, r.Total, m.Total)
}
