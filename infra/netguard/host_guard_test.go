package netguard

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1", "::ffff:127.0.0.1",
		"10.0.0.1", "172.16.0.1", "172.31.255.254", "192.168.1.1",
		"169.254.169.254",
		"100.64.0.1", "0.0.0.0", "255.255.255.255", "224.0.0.1",
		"fc00::1", "fe80::1", "ff02::1", "::",
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q is not a valid IP", s)
		}
		if !IsBlockedIP(ip) {
			t.Errorf("IsBlockedIP(%s) = false, want true (internal address must be unreachable)", s)
		}
	}

	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700::1111"}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q is not a valid IP", s)
		}
		if IsBlockedIP(ip) {
			t.Errorf("IsBlockedIP(%s) = true, want false (public address must stay reachable)", s)
		}
	}

	if !IsBlockedIP(nil) {
		t.Error("IsBlockedIP(nil) = false, want true (an unparseable address must fail closed)")
	}
}

func TestResolvesToBlocked_IPLiterals(t *testing.T) {
	g := New()
	for _, s := range []string{"127.0.0.1", "169.254.169.254", "[::1]", "10.1.2.3"} {
		if !g.ResolvesToBlocked(s) {
			t.Errorf("ResolvesToBlocked(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"8.8.8.8", ""} {
		if g.ResolvesToBlocked(s) {
			t.Errorf("ResolvesToBlocked(%q) = true, want false", s)
		}
	}
}

func TestResolvesToBlocked_UnresolvableIsNotBlocked(t *testing.T) {
	g := New()
	if g.ResolvesToBlocked("this-host-does-not-exist.invalid") {
		t.Error("an unresolvable host must not be reported as blocked")
	}
}
