package emailaddress

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    Address
		wantErr error
	}{
		{name: "lowercases the whole address", raw: "Joao.Silva@Loja.COM.br", want: "joao.silva@loja.com.br"},
		{name: "trims surrounding space", raw: "  ana@loja.com.br \t", want: "ana@loja.com.br"},
		{name: "accepts plus tags", raw: "ana+promo@loja.com.br", want: "ana+promo@loja.com.br"},
		{name: "accepts subdomains", raw: "ana@mail.loja.com.br", want: "ana@mail.loja.com.br"},
		{name: "empty is refused", raw: "   ", wantErr: ErrEmpty},
		{name: "missing at is refused", raw: "ana.loja.com.br", wantErr: ErrInvalid},
		{name: "display name form is refused", raw: "Ana <ana@loja.com.br>", wantErr: ErrInvalid},
		{name: "two ats are refused", raw: "ana@x@loja.com.br", wantErr: ErrInvalid},
		{name: "domain without dot is refused", raw: "ana@localhost", wantErr: ErrInvalid},
		{name: "domain with empty label is refused", raw: "ana@loja..com", wantErr: ErrInvalid},
		{name: "domain label starting with hyphen is refused", raw: "ana@-loja.com", wantErr: ErrInvalid},
		{name: "local part with space is refused", raw: "an a@loja.com", wantErr: ErrInvalid},
		{name: "header injection is refused", raw: "ana@loja.com\r\nBcc: x@y.com", wantErr: ErrInvalid},
		{name: "local part over 64 is refused", raw: repeat("a", 65) + "@loja.com", wantErr: ErrInvalid},
		{name: "address over 254 is refused", raw: "a@" + repeat("b", 250) + ".com", wantErr: ErrInvalid},
		{name: "leading dot in local part is refused", raw: ".ana@loja.com", wantErr: ErrInvalid},
		{name: "numeric top level label is refused", raw: "ana@loja.123", wantErr: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Normalize(%q) error = %v, want %v", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize(%q) unexpected error %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestAddressParts(t *testing.T) {
	a := Address("ana+promo@mail.loja.com.br")
	if a.LocalPart() != "ana+promo" {
		t.Fatalf("LocalPart = %q", a.LocalPart())
	}
	if a.Domain() != "mail.loja.com.br" {
		t.Fatalf("Domain = %q", a.Domain())
	}
}

func TestNormalizeDomain(t *testing.T) {
	cases := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "Loja.com.BR", want: "loja.com.br"},
		{raw: " loja.com.br. ", want: "loja.com.br"},
		{raw: "https://loja.com.br", wantErr: true},
		{raw: "loja", wantErr: true},
		{raw: "", wantErr: true},
		{raw: "lo ja.com", wantErr: true},
		{raw: "*.loja.com", wantErr: true},
	}
	for _, tc := range cases {
		got, err := NormalizeDomain(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("NormalizeDomain(%q) = %q, want error", tc.raw, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("NormalizeDomain(%q) = %q, %v, want %q", tc.raw, got, err, tc.want)
		}
	}
}

func TestIsFreeMailbox(t *testing.T) {
	cases := []struct {
		domain string
		want   bool
	}{
		{domain: "gmail.com", want: true},
		{domain: "GMAIL.COM", want: true},
		{domain: "hotmail.com", want: true},
		{domain: "outlook.com.br", want: true},
		{domain: "yahoo.com.br", want: true},
		{domain: "uol.com.br", want: true},
		{domain: "bol.com.br", want: true},
		{domain: "icloud.com", want: true},
		{domain: "loja.com.br", want: false},
		{domain: "mail.gmail.com.loja.com.br", want: false},
	}
	for _, tc := range cases {
		if got := IsFreeMailbox(tc.domain); got != tc.want {
			t.Fatalf("IsFreeMailbox(%q) = %v, want %v", tc.domain, got, tc.want)
		}
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
