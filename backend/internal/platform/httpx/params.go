package httpx

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	if q.Has("page") {
		n, err := strconv.ParseInt(q.Get("page"), 10, 32)
		v.Check(err == nil && n >= 1, "page", "must be an integer >= 1")
		p.Number = int32(n)
	}
	if q.Has("pageSize") {
		n, err := strconv.ParseInt(q.Get("pageSize"), 10, 32)
		v.Check(err == nil && n >= 1 && n <= pagination.MaxSize, "pageSize", fmt.Sprintf("must be an integer between 1 and %d", pagination.MaxSize))
		p.Size = int32(n)
	}
	if err := v.Err(); err != nil {
		return p, err
	}
	v.Check(int64(p.Number-1)*int64(p.Size) <= pagination.MaxOffset, "page", "is too large for the page size")
	return p, v.Err()
}

// PathID parses a positive int64 path parameter.
func PathID(r *http.Request, name string) (int64, error) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || n < 1 {
		return 0, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: name, Message: "must be a positive integer"})
	}
	return n, nil
}

// EnumQuery reads an optional query parameter restricted to allowed values.
func EnumQuery(r *http.Request, name string, allowed ...string) (*string, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return nil, nil
	}
	raw := q.Get(name)
	for _, a := range allowed {
		if raw == a {
			return &raw, nil
		}
	}
	return nil, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: name, Message: "must be one of " + strings.Join(allowed, ", ")})
}

// PatternQuery reads an optional query parameter that must match re (present but empty is invalid).
func PatternQuery(r *http.Request, name string, re *regexp.Regexp, message string) (*string, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return nil, nil
	}
	raw := q.Get(name)
	if !re.MatchString(raw) {
		return nil, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: name, Message: message})
	}
	return &raw, nil
}

// TextQuery reads an optional free-text query parameter: present but empty, longer than max runes, invalid UTF-8 or
// with NUL characters is invalid.
func TextQuery(r *http.Request, name string, maxRunes int) (*string, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return nil, nil
	}
	raw := q.Get(name)
	var v apperr.Validator
	v.Check(raw != "", name, "must not be empty")
	v.CheckText(name, raw)
	v.Check(utf8.RuneCountInString(raw) <= maxRunes, name, fmt.Sprintf("must be at most %d characters", maxRunes))
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &raw, nil
}

// TimeQuery reads an optional RFC 3339 date-time query parameter (e.g. 2026-10-08T03:00:00Z).
func TimeQuery(r *http.Request, name string) (*time.Time, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, q.Get(name))
	if err != nil {
		return nil, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: name, Message: "must be an RFC 3339 date-time such as 2026-10-08T03:00:00Z"})
	}
	return &t, nil
}
