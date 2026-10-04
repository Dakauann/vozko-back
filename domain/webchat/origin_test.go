package webchat

import (
	"errors"
	"testing"
)

func TestParseOriginPatternAcceptsOnlyExactSiteOrigins(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want string
		err  error
	}{
		"https host":                   {raw: "https://loja.example.com", want: "https://loja.example.com"},
		"trailing slash is dropped":    {raw: "https://loja.example.com/", want: "https://loja.example.com"},
		"case is folded":               {raw: "HTTPS://Loja.Example.COM", want: "https://loja.example.com"},
		"explicit port is kept":        {raw: "https://loja.example.com:8443", want: "https://loja.example.com:8443"},
		"default port is dropped":      {raw: "https://loja.example.com:443", want: "https://loja.example.com"},
		"subdomain wildcard":           {raw: "https://*.example.com", want: "https://*.example.com"},
		"http on localhost":            {raw: "http://localhost:3000", want: "http://localhost:3000"},
		"http on loopback ip":          {raw: "http://127.0.0.1:5173", want: "http://127.0.0.1:5173"},
		"bare star is refused":         {raw: "*", err: ErrOriginInvalid},
		"empty is refused":             {raw: "  ", err: ErrOriginInvalid},
		"http on a public host":        {raw: "http://loja.example.com", err: ErrOriginInsecure},
		"a path is refused":            {raw: "https://loja.example.com/chat", err: ErrOriginInvalid},
		"a query is refused":           {raw: "https://loja.example.com?x=1", err: ErrOriginInvalid},
		"credentials are refused":      {raw: "https://user:pw@loja.example.com", err: ErrOriginInvalid},
		"other schemes are refused":    {raw: "ftp://loja.example.com", err: ErrOriginInvalid},
		"no scheme is refused":         {raw: "loja.example.com", err: ErrOriginInvalid},
		"wildcard on a tld is refused": {raw: "https://*.com", err: ErrOriginInvalid},
		"inner wildcard is refused":    {raw: "https://loja.*.com", err: ErrOriginInvalid},
		"double wildcard is refused":   {raw: "https://*.*.example.com", err: ErrOriginInvalid},
		"wildcard on localhost":        {raw: "http://*.localhost", err: ErrOriginInvalid},
		"spaces inside are refused":    {raw: "https://lo ja.example.com", err: ErrOriginInvalid},
		"csp separators are refused":   {raw: "https://loja.example.com;script-src", err: ErrOriginInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseOriginPattern(tc.raw)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("ParseOriginPattern(%q) error = %v, want %v", tc.raw, err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseOriginPattern(%q) unexpected error %v", tc.raw, err)
			}
			if got.String() != tc.want {
				t.Fatalf("ParseOriginPattern(%q) = %q, want %q", tc.raw, got.String(), tc.want)
			}
		})
	}
}

func TestOriginPatternMatchesOnlyItsOwnSites(t *testing.T) {
	cases := []struct {
		pattern string
		origin  string
		want    bool
	}{
		{"https://loja.example.com", "https://loja.example.com", true},
		{"https://loja.example.com", "https://LOJA.example.com", true},
		{"https://loja.example.com", "https://loja.example.com:443", true},
		{"https://loja.example.com", "http://loja.example.com", false},
		{"https://loja.example.com", "https://loja.example.com:8443", false},
		{"https://loja.example.com", "https://evil-loja.example.com", false},
		{"https://loja.example.com", "https://loja.example.com.evil.io", false},
		{"https://*.example.com", "https://blog.example.com", true},
		{"https://*.example.com", "https://a.b.example.com", true},
		{"https://*.example.com", "https://example.com", false},
		{"https://*.example.com", "https://evilexample.com", false},
		{"https://*.example.com", "null", false},
		{"https://loja.example.com", "", false},
	}
	for _, tc := range cases {
		p, err := ParseOriginPattern(tc.pattern)
		if err != nil {
			t.Fatalf("pattern %q: %v", tc.pattern, err)
		}
		if got := p.Matches(tc.origin); got != tc.want {
			t.Errorf("%q matches %q = %v, want %v", tc.pattern, tc.origin, got, tc.want)
		}
	}
}
