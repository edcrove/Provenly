package apperr

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstructors(t *testing.T) {
	v := Validation("bad", FieldError{Field: "f", Message: "m"})
	e, ok := As(v)
	require.True(t, ok)
	assert.Equal(t, KindValidation, e.Kind)
	assert.Equal(t, "bad", v.Error())
	assert.Equal(t, []FieldError{{Field: "f", Message: "m"}}, e.Fields)

	nf, ok := As(fmt.Errorf("wrapped: %w", NotFound("tc %d", 7)))
	require.True(t, ok)
	assert.Equal(t, KindNotFound, nf.Kind)
	assert.Equal(t, "tc 7", nf.Message)

	doc, ok := As(InvalidDocument("x %s", "y"))
	require.True(t, ok)
	assert.Equal(t, KindInvalidDocument, doc.Kind)

	c, ok := As(Conflict("project %s exists", "CHK"))
	require.True(t, ok)
	assert.Equal(t, KindConflict, c.Kind)
	assert.Equal(t, "project CHK exists", c.Message)

	u, ok := As(Unauthorized("sign in"))
	require.True(t, ok)
	assert.Equal(t, KindUnauthorized, u.Kind)
	f, ok := As(Forbidden("admins only"))
	require.True(t, ok)
	assert.Equal(t, KindForbidden, f.Kind)
	pf, ok := As(PreconditionFailed("changed at %d", 3))
	require.True(t, ok)
	assert.Equal(t, KindPreconditionFailed, pf.Kind)
	assert.Equal(t, "changed at 3", pf.Message)
	tm, ok := As(TooManyRequests("wait %d minutes", 15))
	require.True(t, ok)
	assert.Equal(t, KindTooManyRequests, tm.Kind)
	assert.Equal(t, "wait 15 minutes", tm.Message)
	up, ok := As(Upstream("github answered %d", 401))
	require.True(t, ok)
	assert.Equal(t, KindUpstream, up.Kind)
	assert.Equal(t, "github answered 401", up.Message)

	_, ok = As(errors.New("plain"))
	assert.False(t, ok)
}

func TestValidator(t *testing.T) {
	var v Validator
	v.Check(true, "a", "never")
	require.NoError(t, v.Err())
	v.Check(false, "b", "is wrong")
	e, ok := As(v.Err())
	require.True(t, ok)
	assert.Equal(t, []FieldError{{Field: "b", Message: "is wrong"}}, e.Fields)
}

func TestCheckTextRejectsUnstorableText(t *testing.T) {
	for _, ok := range []string{"", "plain", "ñandú ✓ 😀", "tab\tand\nnewline"} {
		var v Validator
		v.CheckText("f", ok)
		assert.NoError(t, v.Err(), "%q", ok)
	}
	for _, bad := range []string{"a\x00b", "\x00", "bad \xff utf8", "\xc3"} {
		var v Validator
		v.CheckText("f", bad)
		e, ok := As(v.Err())
		require.True(t, ok, "%q", bad)
		assert.Equal(t, []FieldError{{Field: "f", Message: "must be valid UTF-8 text without NUL characters"}}, e.Fields)
	}
}
