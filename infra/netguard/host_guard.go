package netguard

import (
	"net"
	"strings"
)

type HostGuard struct{}

func New() *HostGuard { return &HostGuard{} }

func (g *HostGuard) ResolvesToBlocked(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
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

var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.88.99.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"::/128",
		"::1/128",
		"64:ff9b::/96",
		"100::/64",
		"2001:db8::/32",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
	out := make([]*net.IPNet, 0, len(raw))
	for _, c := range raw {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
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
