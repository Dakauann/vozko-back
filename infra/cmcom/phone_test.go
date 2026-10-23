package cmcom

import (
	"errors"
	"testing"
)

func TestGatewayNumber(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"5511987654321", "005511987654321", true},
		{"+5511987654321", "005511987654321", true},
		{" +31612345678 ", "0031612345678", true},
		{"", "", false},
		{"+", "", false},
		{"005511987654321", "", false},
		{"55 11 98765-4321", "", false},
		{"0511987654321", "", false},
		{"1234567", "", false},
		{"1234567890123456", "", false},
		{"++5511987654321", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := GatewayNumber(tc.in)
			if tc.ok {
				if err != nil || got != tc.want {
					t.Fatalf("GatewayNumber(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
				}
				return
			}
			if !errors.Is(err, ErrPhoneInvalid) {
				t.Fatalf("GatewayNumber(%q) = %q, %v; want ErrPhoneInvalid", tc.in, got, err)
			}
		})
	}
}

func TestPhoneFromGateway(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"0031612345678", "31612345678", true},
		{"+316012345678", "316012345678", true},
		{"5511987654321", "5511987654321", true},
		{"", "", false},
		{"00", "", false},
		{"+00316", "", false},
		{"0031 612345678", "", false},
		{"00cm.com_agent", "", false},
		{"0000316012345678", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := PhoneFromGateway(tc.in)
			if tc.ok {
				if err != nil || got != tc.want {
					t.Fatalf("PhoneFromGateway(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
				}
				return
			}
			if !errors.Is(err, ErrPhoneInvalid) {
				t.Fatalf("PhoneFromGateway(%q) = %q, %v; want ErrPhoneInvalid", tc.in, got, err)
			}
		})
	}
}
