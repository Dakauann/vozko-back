package sip_trunk

import "net/netip"

const (
	narrowestIPv4SourceBits = 16
	narrowestIPv6SourceBits = 32
)

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func IsPublicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() &&
		addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!sharedAddressSpace.Contains(addr) &&
		addr != netip.IPv4Unspecified() &&
		addr != netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

func tooBroad(prefix netip.Prefix) bool {
	if prefix.Addr().Is4() {
		return prefix.Bits() < narrowestIPv4SourceBits
	}
	return prefix.Bits() < narrowestIPv6SourceBits
}

func unmappedBits(prefix netip.Prefix) int {
	if prefix.Addr().Is4In6() {
		return 96
	}
	return 0
}
