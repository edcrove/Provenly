package projectkey

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

func fieldError(t *testing.T, err error) apperr.FieldError {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, apperr.KindValidation, e.Kind)
	require.Len(t, e.Fields, 1)
	return e.Fields[0]
}

func TestValid(t *testing.T) {
	for _, k := range []string{"TC", "CHK", "A1", "ABCDEFGHIJ"} {
		assert.True(t, Valid(k), k)
	}
	for _, k := range []string{"", "T", "chk", "1AB", "ABCDEFGHIJK", "CH-K", " CHK", "CHK\n"} {
		assert.False(t, Valid(k), k)
	}
	var v apperr.Validator
	Check(&v, "key", "x")
	assert.Equal(t, apperr.FieldError{Field: "key", Message: Message}, fieldError(t, v.Err()))
	assert.Equal(t, apperr.FieldError{Field: "project", Message: Message}, fieldError(t, Invalid("project")))
	e, _ := apperr.As(NotFound("CHK"))
	assert.Equal(t, apperr.KindNotFound, e.Kind)
	assert.Equal(t, "project CHK not found", e.Message)
}

func TestPathAndQuery(t *testing.T) {
	serve := func(target string, h func(*http.Request)) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /p/{projectKey}", func(_ http.ResponseWriter, r *http.Request) { h(r) })
		mux.HandleFunc("GET /q", func(_ http.ResponseWriter, r *http.Request) { h(r) })
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	}
	serve("/p/CHK", func(r *http.Request) {
		key, err := Path(r)
		require.NoError(t, err)
		assert.Equal(t, "CHK", key)
	})
	serve("/p/chk", func(r *http.Request) {
		_, err := Path(r)
		assert.Equal(t, "projectKey", fieldError(t, err).Field)
	})
	serve("/q", func(r *http.Request) {
		key, err := Query(r)
		assert.NoError(t, err)
		assert.Nil(t, key)
	})
	serve("/q?project=CHK&project=x", func(r *http.Request) {
		key, err := Query(r)
		require.NoError(t, err)
		assert.Equal(t, "CHK", *key, "the first value wins")
	})
	for _, bad := range []string{"/q?project=", "/q?project=chk"} {
		serve(bad, func(r *http.Request) {
			_, err := Query(r)
			assert.Equal(t, "project", fieldError(t, err).Field, bad)
		})
	}
}

type guard struct{ deny map[int64]bool }

func (g guard) Require(_ context.Context, id int64, _ authz.Role, notFound error) error {
	if g.deny[id] {
		return notFound
	}
	return nil
}

type project struct{ id int64 }

func TestResolve(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("db down")
	lookup := func(_ context.Context, key string) (project, error) {
		switch key {
		case "CHK":
			return project{1}, nil
		case "HID":
			return project{2}, nil
		case "ERR":
			return project{}, boom
		}
		return project{}, apperr.NotFound("no such row")
	}
	id := func(p project) int64 { return p.id }
	g := guard{deny: map[int64]bool{2: true}}

	p, err := Resolve(ctx, "project", "CHK", lookup, id, g, authz.RoleViewer)
	require.NoError(t, err)
	assert.Equal(t, int64(1), p.id)

	_, err = Resolve(ctx, "projectKey", "chk", lookup, id, g, authz.RoleViewer)
	assert.Equal(t, "projectKey", fieldError(t, err).Field)

	for _, key := range []string{"NOPE", "HID"} {
		_, err = Resolve(ctx, "project", key, lookup, id, g, authz.RoleViewer)
		assert.Equal(t, NotFound(key), err, "an unknown and a hidden project read the same: %s", key)
	}
	_, err = Resolve(ctx, "project", "ERR", lookup, id, g, authz.RoleViewer)
	assert.ErrorIs(t, err, boom)
}
