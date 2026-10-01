package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	return p
}

func TestWriteErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
		fields []FieldError
	}{
		{apperr.NotFound("nope"), 404, CodeNotFound, nil},
		{apperr.InvalidDocument("bad xml"), 400, CodeInvalidJUnit, nil},
		{apperr.Validation("v", apperr.FieldError{Field: "title", Message: "req"}), 400, CodeValidation, []FieldError{{Field: "title", Message: "req"}}},
		{&http.MaxBytesError{Limit: 1}, 413, CodePayloadTooLarge, nil},
		{&apperr.Error{Kind: apperr.Kind(99), Message: "odd"}, 500, CodeInternal, nil},
		{errors.New("db down"), 500, CodeInternal, nil},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), c.err)
		assert.Equal(t, c.status, rec.Code)
		p := decodeProblem(t, rec)
		assert.Equal(t, c.code, p.Code)
		assert.Equal(t, c.status, p.Status)
		assert.Equal(t, "about:blank", p.Type)
		assert.Equal(t, http.StatusText(c.status), p.Title)
		assert.Equal(t, c.fields, p.Errors)
	}
	rec := httptest.NewRecorder()
	WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), errors.New("secret internals"))
	assert.NotContains(t, rec.Body.String(), "secret internals")
}

func TestDecodeJSON(t *testing.T) {
	type body struct {
		Title string `json:"title"`
	}
	decode := func(raw string) (body, error) {
		var b body
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
		err := DecodeJSON(httptest.NewRecorder(), r, &b)
		return b, err
	}
	b, err := decode(`{"title":"ok"}`)
	require.NoError(t, err)
	assert.Equal(t, "ok", b.Title)

	for raw, msg := range map[string]string{
		``:                         "request body is required",
		`{"id":1}`:                 "unknown field",
		`{"title":`:                "malformed JSON",
		`{"title":"a"}{"title":1}`: "single JSON object",
	} {
		_, err := decode(raw)
		e, ok := apperr.As(err)
		require.True(t, ok, raw)
		assert.Equal(t, apperr.KindValidation, e.Kind)
		assert.Contains(t, e.Message, msg)
	}

	_, err = decode(`{"title":"` + strings.Repeat("a", maxJSONBody) + `"}`)
	var tooLarge *http.MaxBytesError
	assert.ErrorAs(t, err, &tooLarge)
}

func TestWriteJSONAndNewPage(t *testing.T) {
	rec := httptest.NewRecorder()
	res := pagination.Result[int]{Items: []int{5}, Page: pagination.Page{Number: 1, Size: 1}, Total: 3}
	WriteJSON(rec, http.StatusCreated, NewPage(res, func(i int) int { return i * 2 }))
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"page":1,"pageSize":1,"totalItems":3,"totalPages":3,"items":[10]}`, rec.Body.String())
}

func TestParsePage(t *testing.T) {
	p, err := ParsePage(httptest.NewRequest(http.MethodGet, "/?page=2&pageSize=100", nil))
	require.NoError(t, err)
	assert.Equal(t, pagination.Page{Number: 2, Size: 100}, p)

	p, err = ParsePage(httptest.NewRequest(http.MethodGet, "/", nil))
	require.NoError(t, err)
	assert.Equal(t, pagination.Default(), p)

	for _, q := range []string{"page=0", "page=x", "pageSize=0", "pageSize=101", "pageSize=a"} {
		_, err := ParsePage(httptest.NewRequest(http.MethodGet, "/?"+q, nil))
		require.Error(t, err, q)
	}
}

func TestPathID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", "42")
	id, err := PathID(r, "id")
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)

	for _, v := range []string{"0", "-1", "abc", ""} {
		r.SetPathValue("id", v)
		_, err := PathID(r, "id")
		e, ok := apperr.As(err)
		require.True(t, ok, v)
		assert.Equal(t, "id", e.Fields[0].Field)
	}
}

func TestEnumQuery(t *testing.T) {
	v, err := EnumQuery(httptest.NewRequest(http.MethodGet, "/", nil), "status", "a", "b")
	require.NoError(t, err)
	assert.Nil(t, v)

	v, err = EnumQuery(httptest.NewRequest(http.MethodGet, "/?status=b", nil), "status", "a", "b")
	require.NoError(t, err)
	assert.Equal(t, "b", *v)

	_, err = EnumQuery(httptest.NewRequest(http.MethodGet, "/?status=c", nil), "status", "a", "b")
	require.Error(t, err)
}

func TestMiddleware(t *testing.T) {
	panicking := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	panicking.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, CodeInternal, decodeProblem(t, rec).Code)

	ok := Recover(AccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })))
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTeapot, rec.Code)

}

func TestRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	h := Routes(mux)
	serve := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	assert.Equal(t, http.StatusNoContent, serve(http.MethodGet, "/items").Code)
	assert.Equal(t, http.StatusCreated, serve(http.MethodPost, "/items").Code)

	rec := serve(http.MethodDelete, "/items")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD, POST", rec.Header().Get("Allow"))
	p := decodeProblem(t, rec)
	assert.Equal(t, CodeMethodNotAllowed, p.Code)
	assert.Equal(t, "method DELETE is not allowed for /items", p.Detail)

	rec = serve(http.MethodDelete, "/missing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, rec.Header().Get("Allow"))
	p = decodeProblem(t, rec)
	assert.Equal(t, CodeNotFound, p.Code)
	assert.Equal(t, "no route for DELETE /missing", p.Detail)
}
