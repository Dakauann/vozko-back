package netguard

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fixedResolver map[string][]net.IPAddr

func (r fixedResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addrs, ok := r[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	return addrs, nil
}

func TestCheckHostFailsClosed(t *testing.T) {
	c := NewHostChecker(fixedResolver{
		"hooks.example.com":  {{IP: net.ParseIP("93.184.216.34")}},
		"rebind.example.com": {{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("10.0.0.5")}},
		"empty.example.com":  {},
	})
	cases := []struct {
		host string
		want error
	}{
		{"hooks.example.com", nil},
		{"93.184.216.34", nil},
		{"rebind.example.com", ErrBlockedAddress},
		{"127.0.0.1", ErrBlockedAddress},
		{"169.254.169.254", ErrBlockedAddress},
		{"[::1]", ErrBlockedAddress},
		{"192.168.0.10", ErrBlockedAddress},
		{"missing.example.com", ErrHostUnresolved},
		{"empty.example.com", ErrHostUnresolved},
		{"", ErrHostUnresolved},
	}
	for _, tc := range cases {
		err := c.CheckHost(context.Background(), tc.host)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("CheckHost(%q) = %v, want %v", tc.host, err, tc.want)
		}
	}
}
