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
