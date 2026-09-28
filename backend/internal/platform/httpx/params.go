package httpx

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// PageMeta is the paging metadata of every listing response.
type PageMeta struct {
	Page       int32 `json:"page"`
	PageSize   int32 `json:"pageSize"`
	TotalItems int64 `json:"totalItems"`
	TotalPages int32 `json:"totalPages"`
}

// PageResponse is the body of every listing response.
type PageResponse[T any] struct {
	PageMeta
	Items []T `json:"items"`
}

// NewPage converts a pagination result into its wire form.
func NewPage[T, U any](r pagination.Result[T], f func(T) U) PageResponse[U] {
	m := pagination.Map(r, f)
	return PageResponse[U]{
		PageMeta: PageMeta{Page: r.Page.Number, PageSize: r.Page.Size, TotalItems: r.Total, TotalPages: r.TotalPages()},
		Items:    m.Items,
	}
}

// ParsePage reads the page and pageSize query parameters.
func ParsePage(r *http.Request) (pagination.Page, error) {
	p := pagination.Default()
	var v apperr.Validator
	q := r.URL.Query()
	if raw := q.Get("page"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		v.Check(err == nil && n >= 1, "page", "must be an integer >= 1")
		p.Number = int32(n)
	}
	if raw := q.Get("pageSize"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		v.Check(err == nil && n >= 1 && n <= pagination.MaxSize, "pageSize", fmt.Sprintf("must be an integer between 1 and %d", pagination.MaxSize))
		p.Size = int32(n)
	}
	return p, v.Err()
}

// PathID parses a positive int64 path parameter.
func PathID(r *http.Request, name string) (int64, error) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || n < 1 {
		return 0, apperr.Validation("invalid path parameter", apperr.FieldError{Field: name, Message: "must be a positive integer"})
	}
	return n, nil
}

// EnumQuery reads an optional query parameter restricted to allowed values.
func EnumQuery(r *http.Request, name string, allowed ...string) (*string, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	for _, a := range allowed {
		if raw == a {
			return &raw, nil
		}
	}
	return nil, apperr.Validation("invalid query parameter", apperr.FieldError{Field: name, Message: fmt.Sprintf("must be one of %v", allowed)})
}
