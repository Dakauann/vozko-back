package netguard

import "testing"

func TestResolvesToBlocked_IPLiterals(t *testing.T) {
	g := New()
	cases := map[string]bool{
		"127.0.0.1":       true,
		"10.0.0.1":        true,
		"192.168.1.1":     true,
		"172.16.0.1":      true,
		"169.254.169.254": true, // cloud metadata
		"::1":             true,
		"0.0.0.0":         true,
		"8.8.8.8":         false, // public
		"1.1.1.1":         false,
		"":                false,
	}
	for host, want := range cases {
		t.Run(host, func(t *testing.T) {
			if got := g.ResolvesToBlocked(host); got != want {
				t.Fatalf("ResolvesToBlocked(%q) = %v, want %v", host, got, want)
			}
		})
	}
}
