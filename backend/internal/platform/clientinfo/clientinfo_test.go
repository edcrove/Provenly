package clientinfo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seen(t *testing.T, trusted string, remote string, xff []string, ua string) Info {
	t.Helper()
	nets, err := ParseTrusted(trusted)
	require.NoError(t, err)
	var got Info
	h := Middleware(nets, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = From(r.Context()) }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	r.Header.Set("User-Agent", ua)
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

// X-Forwarded-For is believed only from trusted proxies (card #49).
func TestClientIP(t *testing.T) {
	assert.Equal(t, Info{IP: "203.0.113.9", UserAgent: "curl/8"}, seen(t, "", "203.0.113.9:5000", []string{"1.2.3.4"}, "curl/8"),
		"no trusted proxy: the header is ignored")
	assert.Equal(t, "198.51.100.7", seen(t, "10.0.0.0/8", "10.0.0.2:80", []string{"1.2.3.4, 198.51.100.7, 10.0.0.5"}, "").IP,
		"the nearest untrusted hop; a spoofed leftmost one is skipped")
	assert.Equal(t, "198.51.100.7", seen(t, "10.0.0.2", "10.0.0.2:80", []string{"1.2.3.4", "198.51.100.7"}, "").IP, "several headers")
	assert.Equal(t, "10.0.0.3", seen(t, "10.0.0.0/8", "10.0.0.2:80", []string{"10.0.0.3"}, "").IP, "only proxies: the farthest")
	assert.Equal(t, "10.0.0.2", seen(t, "10.0.0.0/8", "10.0.0.2:80", []string{"garbage"}, "").IP, "an unreadable hop stops the walk")
	assert.Equal(t, "203.0.113.9", seen(t, "::ffff:10.0.0.2", "[::ffff:10.0.0.2]:80", []string{"203.0.113.9"}, "").IP, "IPv4-mapped")
	assert.Equal(t, "", seen(t, "", "pipe", nil, "").IP, "an address that is not IP")
	long := seen(t, "", "1.1.1.1:1", nil, strings.Repeat("é", 400)+"\xff").UserAgent
	assert.LessOrEqual(t, len(long), maxUserAgent)
	assert.NotContains(t, long, "\xff")
	assert.Equal(t, Info{}, From(context.Background()))
}

func TestParseTrusted(t *testing.T) {
	nets, err := ParseTrusted(" 10.0.0.0/8, 192.168.1.1 ,,::1 ")
	require.NoError(t, err)
	var got []string
	for _, n := range nets {
		got = append(got, n.String())
	}
	assert.Equal(t, []string{"10.0.0.0/8", "192.168.1.1/32", "::1/128"}, got)
	_, err = ParseTrusted("10.0.0.0/8,nope")
	assert.ErrorContains(t, err, `"nope" is not an IP address or CIDR range`)
}
