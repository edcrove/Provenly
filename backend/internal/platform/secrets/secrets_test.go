package secrets

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSealOpen(t *testing.T) {
	box, err := New(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	a, b := box.Seal("ghp_token"), box.Seal("ghp_token")
	assert.True(t, strings.HasPrefix(a, "v1:"))
	assert.NotEqual(t, a, b, "a random nonce per value")
	assert.NotContains(t, a, "ghp_token")
	plain, err := box.Open(a)
	require.NoError(t, err)
	assert.Equal(t, "ghp_token", plain)

	other, err := New(nil)
	require.NoError(t, err)
	for _, stored := range []string{"ghp_token", "v1:%%%", "v1:" + "AAAA", a[:len(a)-4] + "AAAA"} {
		_, err := box.Open(stored)
		assert.ErrorIs(t, err, ErrUndecryptable, stored)
	}
	_, err = other.Open(a)
	assert.ErrorIs(t, err, ErrUndecryptable, "another key")
	_, err = New([]byte("short"))
	assert.ErrorContains(t, err, "secrets key")
}
