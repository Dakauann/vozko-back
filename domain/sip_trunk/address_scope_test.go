package sip_trunk

import (
	"errors"
	"net/netip"
	"testing"
)

func TestInboundSourcesRefuseRangesThatOpenTheTrunkToTheInternet(t *testing.T) {
	for _, entry := range []string{"0.0.0.0/0", "10.0.0.0/8", "203.0.0.0/15", "::/0", "2001:db8::/31"} {
		if _, err := ParseInboundSources([]string{entry}); !errors.Is(err, ErrInboundSourceTooBroad) {
			t.Errorf("ParseInboundSources(%q) = %v, want ErrInboundSourceTooBroad", entry, err)
		}
	}
	for _, entry := range []string{"203.0.0.0/16", "203.0.113.0/24", "198.51.100.7", "2001:db8::/32", "2001:db8::1"} {
		if _, err := ParseInboundSources([]string{entry}); err != nil {
			t.Errorf("ParseInboundSources(%q) = %v, want nil", entry, err)
		}
	}
	if !IsInvalidInput(ErrInboundSourceTooBroad) {
		t.Fatal("a too broad source must be reported as invalid input")
	}
}

func TestOnlyPublicAddressesAreReachableTrunkHosts(t *testing.T) {
	cases := map[string]bool{
		"203.0.113.10":    true,
		"8.8.8.8":         true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"10.1.2.3":        false,
		"172.16.0.9":      false,
		"192.168.1.1":     false,
		"169.254.169.254": false,
		"100.64.0.1":      false,
		"0.0.0.0":         false,
		"224.0.0.1":       false,
		"255.255.255.255": false,
		"::1":             false,
		"fe80::1":         false,
		"fd00::1":         false,
		"::ffff:10.0.0.1": false,
	}
	for raw, want := range cases {
		if got := IsPublicAddress(netip.MustParseAddr(raw)); got != want {
			t.Errorf("IsPublicAddress(%s) = %v, want %v", raw, got, want)
		}
	}
}
