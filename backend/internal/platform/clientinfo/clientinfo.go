// Package clientinfo records who is on the other end of a request, for the audit log (card #49): the client IP and
// user agent. X-Forwarded-For is believed only from the proxies PROVENLY_TRUSTED_PROXIES names: otherwise any client
// could write the address the log keeps.
package clientinfo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type key struct{}

// Info is the client of a request.
type Info struct {
	IP        string
	UserAgent string
}

// From returns the client recorded in ctx (empty outside a request).
func From(ctx context.Context) Info {
	i, _ := ctx.Value(key{}).(Info)
	return i
}

// With returns ctx carrying i.
func With(ctx context.Context, i Info) context.Context { return context.WithValue(ctx, key{}, i) }

// maxUserAgent bounds the user agent kept (the audit log stores at most 500 characters).
const maxUserAgent = 500

// Middleware records each request's client: the peer address, or, when the peer is a trusted proxy, the nearest
// X-Forwarded-For address that is not one.
func Middleware(trusted []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := strings.ToValidUTF8(r.UserAgent(), "?")
		if len(ua) > maxUserAgent {
			ua = strings.ToValidUTF8(ua[:maxUserAgent], "")
		}
		next.ServeHTTP(w, r.WithContext(With(r.Context(), Info{IP: clientIP(r, trusted), UserAgent: ua})))
	})
}

func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	peer = peer.Unmap()
	if !isTrusted(peer, trusted) {
		return peer.String()
	}
	// Walk X-Forwarded-For from the right: the first address a trusted proxy did not add is the client.
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		a = a.Unmap()
		if !isTrusted(a, trusted) {
			return a.String()
		}
		peer = a
	}
	return peer.String()
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ParseTrusted reads a comma-separated list of addresses and CIDR ranges (PROVENLY_TRUSTED_PROXIES).
func ParseTrusted(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(raw, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR range", f)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}
