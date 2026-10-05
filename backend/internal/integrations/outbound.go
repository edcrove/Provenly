package integrations

import (
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

// errBlockedAddress refuses outbound connections to the deployment's own network (SSRF through webhooks or
// connectors, Notion 17 threat model).
var errBlockedAddress = errors.New("refusing a private, loopback or link-local address")

// blocked tells whether an address belongs to the deployment's own network.
func blocked(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast()
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
