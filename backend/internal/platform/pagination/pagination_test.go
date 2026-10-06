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

func TestSlice(t *testing.T) {
	all := []int{1, 2, 3, 4, 5}
	r := Slice(all, Page{Number: 2, Size: 2})
	if len(r.Items) != 2 || r.Items[0] != 3 || r.Total != 5 || r.TotalPages() != 3 {
		t.Fatalf("page 2: %+v", r)
	}
	if r = Slice(all, Page{Number: 3, Size: 2}); len(r.Items) != 1 || r.Items[0] != 5 {
		t.Fatalf("last page: %+v", r)
	}
	if r = Slice(all, Page{Number: 1 << 30, Size: 100}); len(r.Items) != 0 || r.Total != 5 {
		t.Fatalf("past the end: %+v", r)
	}
	if r = Slice([]int(nil), Default()); len(r.Items) != 0 || r.TotalPages() != 0 {
		t.Fatalf("empty: %+v", r)
	}
}
