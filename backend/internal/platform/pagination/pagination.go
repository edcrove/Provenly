// Package pagination implements the page/pageSize convention of the API contract.
package pagination

const (
	// DefaultSize is used when the client does not request a page size.
	DefaultSize = 20
	// MaxSize is the largest page size a client may request.
	MaxSize = 100
	// MaxOffset is the largest SQL OFFSET a page may reach (it must fit the int32 query parameter).
	MaxOffset = 1<<31 - 1
)

// Page is a validated 1-based page request.
type Page struct {
	Number int32
	Size   int32
}

// Default returns the first page with the default size.
func Default() Page { return Page{Number: 1, Size: DefaultSize} }

// Limit is the SQL LIMIT for the page.
func (p Page) Limit() int32 { return p.Size }

// Offset is the SQL OFFSET for the page.
func (p Page) Offset() int32 { return (p.Number - 1) * p.Size }

// Result is one page of items plus the total number of matching items.
type Result[T any] struct {
	Items []T
	Page  Page
	Total int64
}

// TotalPages is the number of pages needed for Total items (0 when empty).
func (r Result[T]) TotalPages() int32 {
	return int32((r.Total + int64(r.Page.Size) - 1) / int64(r.Page.Size))
}

// Map converts the items of a result while keeping its paging metadata.
func Map[T, U any](r Result[T], f func(T) U) Result[U] {
	items := make([]U, len(r.Items))
	for i, it := range r.Items {
		items[i] = f(it)
	}
	return Result[U]{Items: items, Page: r.Page, Total: r.Total}
}
