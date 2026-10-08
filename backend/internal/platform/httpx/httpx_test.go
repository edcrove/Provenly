package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

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
		{apperr.Conflict("taken"), 409, CodeConflict, nil},
		{apperr.Unauthorized("sign in"), 401, CodeUnauthorized, nil},
		{apperr.Forbidden("no"), 403, CodeForbidden, nil},
		{apperr.PreconditionFailed("stale"), 412, CodePreconditionFailed, nil},
		{apperr.Upstream("github down"), 502, CodeUpstream, nil},
		{apperr.TooManyRequests("wait"), 429, CodeTooManyRequests, nil},
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
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
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

func TestPatternQuery(t *testing.T) {
	re := regexp.MustCompile(`^[A-Z]{2}$`)
	v, err := PatternQuery(httptest.NewRequest(http.MethodGet, "/", nil), "project", re, "two letters")
	require.NoError(t, err)
	assert.Nil(t, v)

	v, err = PatternQuery(httptest.NewRequest(http.MethodGet, "/?project=AB&project=x", nil), "project", re, "two letters")
	require.NoError(t, err)
	assert.Equal(t, "AB", *v, "the first repeated value wins")

	for _, target := range []string{"/?project=", "/?project=abc"} {
		_, err = PatternQuery(httptest.NewRequest(http.MethodGet, target, nil), "project", re, "two letters")
		e, ok := apperr.As(err)
		require.True(t, ok, target)
		assert.Equal(t, []apperr.FieldError{{Field: "project", Message: "two letters"}}, e.Fields)
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

func TestTextQuery(t *testing.T) {
	v, err := TextQuery(httptest.NewRequest(http.MethodGet, "/", nil), "branch", 5)
	require.NoError(t, err)
	assert.Nil(t, v)
	v, err = TextQuery(httptest.NewRequest(http.MethodGet, "/?branch=ma%20in&branch=x", nil), "branch", 5)
	require.NoError(t, err)
	assert.Equal(t, "ma in", *v, "the first repeated value wins")
	for _, target := range []string{"/?branch=", "/?branch=%00", "/?branch=%ff", "/?branch=abcdef"} {
		_, err = TextQuery(httptest.NewRequest(http.MethodGet, target, nil), "branch", 5)
		e, ok := apperr.As(err)
		require.True(t, ok, target)
		assert.Equal(t, "branch", e.Fields[0].Field, target)
	}
}

func TestTimeQuery(t *testing.T) {
	v, err := TimeQuery(httptest.NewRequest(http.MethodGet, "/", nil), "from")
	require.NoError(t, err)
	assert.Nil(t, v)
	v, err = TimeQuery(httptest.NewRequest(http.MethodGet, "/?from=2026-10-08T03:00:00-03:00", nil), "from")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC), v.UTC())
	for _, target := range []string{"/?from=", "/?from=2026-10-08", "/?from=now"} {
		_, err = TimeQuery(httptest.NewRequest(http.MethodGet, target, nil), "from")
		e, ok := apperr.As(err)
		require.True(t, ok, target)
		assert.Equal(t, []apperr.FieldError{{Field: "from", Message: "must be an RFC 3339 date-time such as 2026-10-08T03:00:00Z"}}, e.Fields)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
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

func TestDecodeJSONRequiresJSONContentType(t *testing.T) {
	var b struct{ Title string }
	for _, ct := range []string{"", "text/plain", "application/jsonx", "application/x-www-form-urlencoded", "not a media type;"} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"a"}`))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		err := DecodeJSON(httptest.NewRecorder(), r, &b)
		require.ErrorIs(t, err, ErrUnsupportedMediaType, ct)
		rec := httptest.NewRecorder()
		WriteError(rec, r, err)
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
		assert.Equal(t, CodeUnsupportedMediaType, decodeProblem(t, rec).Code)
	}
	// Duplicate keys: the last value wins (encoding/json semantics).
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"a","title":"b"}`))
	r.Header.Set("Content-Type", "Application/JSON")
	require.NoError(t, DecodeJSON(httptest.NewRecorder(), r, &b))
	assert.Equal(t, "b", b.Title)
}

func TestQueryEdgeCases(t *testing.T) {
	get := func(q string) *http.Request { return httptest.NewRequest(http.MethodGet, "/?"+q, nil) }

	// The offset must fit the int32 SQL parameter: a page past it is rejected, never a 500.
	p, err := ParsePage(get("page=21474838&pageSize=100"))
	assert.Equal(t, int32(21474838), p.Number)
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, "page", e.Fields[0].Field)
	for _, q := range []string{"page=21474837&pageSize=100", "page=2147483647&pageSize=1", "page=21474836&pageSize=100"} {
		p, err := ParsePage(get(q))
		require.NoError(t, err, q)
		assert.LessOrEqual(t, int64(p.Offset()), int64(pagination.MaxOffset), q)
		assert.GreaterOrEqual(t, p.Offset(), int32(0), q)
	}
	// Leading zeros are accepted; the first of repeated parameters wins.
	p, err = ParsePage(get("page=02&pageSize=5&pageSize=1"))
	require.NoError(t, err)
	assert.Equal(t, pagination.Page{Number: 2, Size: 5}, p)
	for _, q := range []string{"pageSize=5.0", "pageSize=+5", "pageSize=1e1", "pageSize=%205", "page=99999999999999999999"} {
		_, err := ParsePage(get(q))
		require.Error(t, err, q)
	}

	// A known parameter that is present but empty is invalid.
	for _, q := range []string{"page=", "pageSize="} {
		_, err := ParsePage(get(q))
		require.Error(t, err, q)
	}

	// Enums: an empty value is invalid, values are case-sensitive, the first repeated value wins.
	_, err = EnumQuery(get("status="), "status", "a", "b")
	require.Error(t, err)
	v, err := EnumQuery(get("status=b&status=a"), "status", "a", "b")
	require.NoError(t, err)
	assert.Equal(t, "b", *v)
	_, err = EnumQuery(get("status=A"), "status", "a", "b")
	e, ok = apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.ValidationFailed, e.Message)
	assert.Equal(t, "must be one of a, b", e.Fields[0].Message)
}

func TestPathIDEdgeCases(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for raw, want := range map[string]int64{"01": 1, "+1": 1, "9223372036854775807": 9223372036854775807} {
		r.SetPathValue("id", raw)
		id, err := PathID(r, "id")
		require.NoError(t, err, raw)
		assert.Equal(t, want, id)
	}
	for _, raw := range []string{"9223372036854775808", "1.0", " 1", "1 ", "0x1", "-0"} {
		r.SetPathValue("id", raw)
		_, err := PathID(r, "id")
		e, ok := apperr.As(err)
		require.True(t, ok, raw)
		assert.Equal(t, apperr.ValidationFailed, e.Message)
	}
}

// FuzzParsePage: any query string yields either a validation error or a page
// whose offset and size are within the bounds the SQL layer accepts.
func FuzzParsePage(f *testing.F) {
	for _, s := range []string{"", "page=1", "page=2147483647&pageSize=100", "pageSize=0", "page=-1", "page=1e9&pageSize=%20", "page=21474837&pageSize=100"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, q string) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.URL.RawQuery = q
		p, err := ParsePage(r)
		if err != nil {
			_, ok := apperr.As(err)
			require.True(t, ok)
			return
		}
		require.GreaterOrEqual(t, p.Number, int32(1))
		require.True(t, p.Size >= 1 && p.Size <= pagination.MaxSize)
		require.GreaterOrEqual(t, p.Offset(), int32(0))
	})
}

// FuzzPathID: any path value yields either a validation error or a positive id.
func FuzzPathID(f *testing.F) {
	for _, s := range []string{"1", "0", "-1", "01", "9223372036854775808", "abc", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetPathValue("id", raw)
		id, err := PathID(r, "id")
		if err != nil {
			_, ok := apperr.As(err)
			require.True(t, ok)
			return
		}
		require.Positive(t, id)
	})
}
