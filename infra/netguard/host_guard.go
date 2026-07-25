// Package netguard implements the sip_trunk.HostGuard port: it resolves a host
// and reports whether it points at an internal/blocked address. This is the
// resolve-time half of the SIP-trunk SSRF defense (the create-time half rejects
// blocked IP literals in the domain validation).
package netguard

import (
	"net"
	"strings"

	"vozko/domain/sip_trunk"
)

// HostGuard resolves hostnames via the system resolver. Stateless and safe for
// concurrent use.
type HostGuard struct{}

func New() *HostGuard { return &HostGuard{} }

// ResolvesToBlocked implements sip_trunk.HostGuard. An IP literal is checked
// directly; a hostname is resolved and blocked if any of its addresses falls in
// a blocked range. An unresolvable host is treated as not blocked: it cannot be
// used to reach an internal service, and failing closed would reject legitimate
// trunks whose DNS is briefly unavailable at create time.
func (g *HostGuard) ResolvesToBlocked(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return sip_trunk.IsBlockedIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if sip_trunk.IsBlockedIP(ip) {
			return true
		}
	}
	return false
}
