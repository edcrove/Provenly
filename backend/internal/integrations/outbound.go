package integrations

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// errBlockedAddress refuses outbound connections to the deployment's own network (SSRF through webhooks or
// connectors, Notion 17 threat model).
var errBlockedAddress = errors.New("refusing a private, loopback or link-local address")

// specialPurpose are the non-global ranges of RFC 6890 that the net.IP predicates do not cover, plus cloud
// metadata and host endpoints reachable only from inside a provider's network.
var specialPurpose = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8",        // "this network"
		"100.64.0.0/10",    // shared address space (CGNAT, Tailscale; Alibaba metadata 100.100.100.200)
		"192.0.0.0/24",     // IETF protocol assignments
		"192.0.2.0/24",     // documentation
		"198.18.0.0/15",    // benchmarking
		"198.51.100.0/24",  // documentation
		"203.0.113.0/24",   // documentation
		"240.0.0.0/4",      // reserved, including the broadcast address
		"168.63.129.16/32", // Azure host endpoint (WireServer)
		"100::/64",         // discard-only
		"2001::/32",        // Teredo (embeds an obfuscated IPv4 address)
		"2001:db8::/32",    // documentation
		"fec0::/10",        // deprecated site-local
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

// embeddedIPv4 returns the IPv4 address an IPv6 address carries through NAT64 (64:ff9b::/96, 64:ff9b:1::/48) or
// 6to4 (2002::/16), so a translated address cannot reach a blocked IPv4 one.
func embeddedIPv4(a netip.Addr) (netip.Addr, bool) {
	if !a.Is6() {
		return netip.Addr{}, false
	}
	b := a.As16()
	switch {
	case netip.MustParsePrefix("64:ff9b::/96").Contains(a), netip.MustParsePrefix("64:ff9b:1::/48").Contains(a):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case netip.MustParsePrefix("2002::/16").Contains(a):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

// blocked tells whether an address belongs to the deployment's own network (or to a range that is not globally
// reachable); IPv4-mapped, NAT64 and 6to4 addresses are judged by the IPv4 address they carry.
func blocked(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true // not an address at all
	}
	a = a.Unmap()
	for _, p := range specialPurpose {
		if p.Contains(a) {
			return true
		}
	}
	if v4, ok := embeddedIPv4(a); ok {
		return blocked(net.IP(v4.AsSlice()))
	}
	return false
}

// checkDial refuses blocked addresses after DNS resolution (so a public name pointing inside is refused too).
func checkDial(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || blocked(ip) {
		return errBlockedAddress
	}
	return nil
}

// outboundClient is the HTTP client of webhooks and connectors: short timeouts, no redirects (a redirect could point
// inside), no environment proxy, and private addresses refused unless allowPrivate.
func outboundClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = checkDial
	}
	return &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second, MaxIdleConnsPerHost: 4},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
