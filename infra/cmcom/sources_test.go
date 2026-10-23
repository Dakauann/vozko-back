package cmcom

import (
	"net/netip"
	"testing"
)

func TestAllowedSourceCIDRs(t *testing.T) {
	want := []string{"31.169.56.0/21", "85.119.48.0/21", "188.94.184.0/22"}
	got := AllowedSourceCIDRs()
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, p := range got {
		if p.String() != want[i] {
			t.Fatalf("prefix %d = %s, want %s", i, p, want[i])
		}
	}
	got[0] = netip.MustParsePrefix("0.0.0.0/0")
	if AllowedSourceCIDRs()[0].String() != want[0] {
		t.Fatalf("callers must not be able to change the allowlist")
	}
}

func TestFromAllowedSource(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"31.169.56.0", true},
		{"31.169.63.255", true},
		{"31.169.64.0", false},
		{"31.169.55.255", false},
		{"85.119.48.1", true},
		{"85.119.55.254", true},
		{"85.119.56.0", false},
		{"188.94.184.10", true},
		{"188.94.187.255", true},
		{"188.94.188.0", false},
		{"::ffff:31.169.57.1", true},
		{"::ffff:10.0.0.1", false},
		{"127.0.0.1", false},
		{"2001:db8::1", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			if got := FromAllowedSource(netip.MustParseAddr(tc.ip)); got != tc.want {
				t.Fatalf("FromAllowedSource(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
	if FromAllowedSource(netip.Addr{}) {
		t.Fatalf("the zero address must never be allowed")
	}
}
