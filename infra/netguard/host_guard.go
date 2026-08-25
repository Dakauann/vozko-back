// Package netguard implements the shortlink.HostGuard port: it resolves a host
// and reports whether it points at an internal/blocked address. This is the
// resolve-time half of the SSRF defense (the create-time half rejects blocked IP
// literals in the domain validation).
package netguard

import (
	"net"
	"strings"
)

// HostGuard resolves hostnames via the system resolver. Stateless and safe for
// concurrent use.
type HostGuard struct{}

func New() *HostGuard { return &HostGuard{} }

// ResolvesToBlocked reports whether host points at an address we refuse to reach.
// An IP literal is checked directly; a hostname is resolved and blocked if ANY of
// its addresses falls in a blocked range (a DNS-rebinding host that mixes a public
// and a private answer is blocked). An unresolvable host is treated as not
// blocked: it cannot be used to reach an internal service, and failing closed
// would reject legitimate targets whose DNS is briefly unavailable.
func (g *HostGuard) ResolvesToBlocked(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	// A bracketed IPv6 literal ("[::1]") is still an IP literal.
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if ip := net.ParseIP(host); ip != nil {
		return IsBlockedIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if IsBlockedIP(ip) {
			return true
		}
	}
	return false
}

// blockedCIDRs are the ranges that must never be reachable through a user-supplied
// host: anything that resolves inside them addresses our own infrastructure or the
// host's own network rather than the public internet.
var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		"0.0.0.0/8",       // "this network"
		"10.0.0.0/8",      // RFC 1918 private
		"100.64.0.0/10",   // RFC 6598 carrier-grade NAT
		"127.0.0.0/8",     // loopback
		"169.254.0.0/16",  // link-local, incl. the 169.254.169.254 cloud metadata endpoint
		"172.16.0.0/12",   // RFC 1918 private
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"192.88.99.0/24",  // deprecated 6to4 relay anycast
		"192.168.0.0/16",  // RFC 1918 private
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"224.0.0.0/4",     // multicast
		"240.0.0.0/4",     // reserved, incl. 255.255.255.255 broadcast
		"::/128",          // unspecified
		"::1/128",         // IPv6 loopback
		"64:ff9b::/96",    // IPv4/IPv6 translation
		"100::/64",        // discard-only
		"2001:db8::/32",   // documentation
		"fc00::/7",        // IPv6 unique local
		"fe80::/10",       // IPv6 link-local
		"ff00::/8",        // IPv6 multicast
	}
	out := make([]*net.IPNet, 0, len(raw))
	for _, c := range raw {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// IsBlockedIP reports whether ip sits in a range that must not be reachable
// through a user-supplied host. An IPv4-mapped IPv6 address is normalized first,
// so "::ffff:127.0.0.1" is blocked exactly like "127.0.0.1".
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true // an unparseable address is not something we will reach out to
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
