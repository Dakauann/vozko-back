package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

var ErrHostUnresolved = errors.New("netguard: the host does not resolve to any address")

type IPResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type HostChecker struct {
	resolver IPResolver
}

func NewHostChecker(resolver IPResolver) *HostChecker {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &HostChecker{resolver: resolver}
}

func (c *HostChecker) CheckHost(ctx context.Context, host string) error {
	host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(host), "["), "]")
	if host == "" {
		return ErrHostUnresolved
	}
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedIP(ip) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
		}
		return nil
	}
	addrs, err := c.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHostUnresolved, err)
	}
	if len(addrs) == 0 {
		return ErrHostUnresolved
	}
	for _, a := range addrs {
		if IsBlockedIP(a.IP) {
			return fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, host, a.IP)
		}
	}
	return nil
}
