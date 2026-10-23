package dnslookup

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeResolver struct {
	records map[string][]string
	err     error
	asked   string
}

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	f.asked = name
	if f.err != nil {
		return nil, f.err
	}
	return f.records[name], nil
}

func TestLookupDMARC(t *testing.T) {
	cases := []struct {
		name     string
		resolver *fakeResolver
		want     string
		wantErr  bool
	}{
		{"found", &fakeResolver{records: map[string][]string{"_dmarc.loja.com.br": {"google-site-verification=x", "v=DMARC1; p=quarantine; rua=mailto:d@loja.com.br"}}}, "v=DMARC1; p=quarantine; rua=mailto:d@loja.com.br", false},
		{"case insensitive tag", &fakeResolver{records: map[string][]string{"_dmarc.loja.com.br": {"V=dmarc1; p=none"}}}, "V=dmarc1; p=none", false},
		{"only other txt", &fakeResolver{records: map[string][]string{"_dmarc.loja.com.br": {"hello"}}}, "", false},
		{"nxdomain", &fakeResolver{err: &net.DNSError{Err: "no such host", Name: "_dmarc.loja.com.br", IsNotFound: true}}, "", false},
		{"servfail", &fakeResolver{err: &net.DNSError{Err: "server misbehaving", Name: "_dmarc.loja.com.br", IsTemporary: true}}, "", true},
		{"other error", &fakeResolver{err: errors.New("boom")}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.resolver).LookupDMARC(context.Background(), "Loja.com.br")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if tc.resolver.asked != "_dmarc.loja.com.br" {
				t.Fatalf("asked %q", tc.resolver.asked)
			}
		})
	}
	if _, err := New(&fakeResolver{}).LookupDMARC(context.Background(), "not a domain"); err == nil {
		t.Fatal("invalid domain must fail")
	}
}
