package dnslookup

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
)

type fakeRecordResolver struct {
	fakeResolver
	mx    map[string][]*net.MX
	mxErr error
}

func (f *fakeRecordResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	f.asked = name
	if f.mxErr != nil {
		return nil, f.mxErr
	}
	return f.mx[name], nil
}

func TestLookupMX(t *testing.T) {
	notFound := &net.DNSError{Err: "no such host", Name: "reply.loja.com.br", IsNotFound: true}
	cases := []struct {
		name     string
		resolver *fakeRecordResolver
		want     []string
		wantErr  bool
	}{
		{"hosts in preference order", &fakeRecordResolver{mx: map[string][]*net.MX{"reply.loja.com.br": {{Host: "B.Inbound.Test.", Pref: 20}, {Host: "a.inbound.test.", Pref: 10}}}}, []string{"a.inbound.test", "b.inbound.test"}, false},
		{"nxdomain is no record", &fakeRecordResolver{mxErr: notFound}, nil, false},
		{"servfail is an error", &fakeRecordResolver{mxErr: &net.DNSError{Err: "server misbehaving", IsTemporary: true}}, nil, true},
		{"other error", &fakeRecordResolver{mxErr: errors.New("boom")}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewRecords(tc.resolver).LookupMX(context.Background(), "Reply.Loja.com.br.")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if tc.resolver.asked != "reply.loja.com.br" {
				t.Fatalf("asked %q", tc.resolver.asked)
			}
		})
	}
}

func TestLookupTXT(t *testing.T) {
	cases := []struct {
		name     string
		resolver *fakeRecordResolver
		want     []string
		wantErr  bool
	}{
		{"values", &fakeRecordResolver{fakeResolver: fakeResolver{records: map[string][]string{"_dmarc.loja.com.br": {" v=DMARC1; p=none; ", "other"}}}}, []string{"v=DMARC1; p=none;", "other"}, false},
		{"nxdomain is no record", &fakeRecordResolver{fakeResolver: fakeResolver{err: &net.DNSError{Err: "no such host", IsNotFound: true}}}, nil, false},
		{"servfail is an error", &fakeRecordResolver{fakeResolver: fakeResolver{err: &net.DNSError{Err: "server misbehaving", IsTemporary: true}}}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewRecords(tc.resolver).LookupTXT(context.Background(), "_DMARC.loja.com.br")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if tc.resolver.asked != "_dmarc.loja.com.br" {
				t.Fatalf("asked %q", tc.resolver.asked)
			}
		})
	}
}

func TestLookupRefusesAnEmptyName(t *testing.T) {
	r := NewRecords(&fakeRecordResolver{})
	if _, err := r.LookupMX(context.Background(), " "); err == nil {
		t.Fatal("an empty name must fail")
	}
	if _, err := r.LookupTXT(context.Background(), ""); err == nil {
		t.Fatal("an empty name must fail")
	}
}
