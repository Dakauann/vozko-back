package sip_trunk

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

type InboundSources []netip.Prefix

func ParseInboundSources(raw []string) (InboundSources, error) {
	sources := make(InboundSources, 0, len(raw))
	for _, entry := range raw {
		prefix, err := parseInboundSource(strings.TrimSpace(entry))
		if err != nil {
			return nil, err
		}
		sources = append(sources, prefix)
	}
	return sources, nil
}

func parseInboundSource(entry string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		return prefix.Masked(), nil
	}
	if addr, err := netip.ParseAddr(entry); err == nil {
		return netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()), nil
	}
	return netip.Prefix{}, fmt.Errorf("%w: %q", ErrInvalidInboundSource, entry)
}

func (s InboundSources) Contains(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range s {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (s InboundSources) With(ips ...net.IP) InboundSources {
	out := append(InboundSources(nil), s...)
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			addr = addr.Unmap()
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return out
}
