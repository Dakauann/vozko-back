package cmcom

import "net/netip"

var allowedSources = []netip.Prefix{
	netip.MustParsePrefix("31.169.56.0/21"),
	netip.MustParsePrefix("85.119.48.0/21"),
	netip.MustParsePrefix("188.94.184.0/22"),
}

func AllowedSourceCIDRs() []netip.Prefix {
	return append([]netip.Prefix(nil), allowedSources...)
}

func FromAllowedSource(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	for _, prefix := range allowedSources {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
