package emailsending

import "testing"

func TestSlugCandidate(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "Loja do João", want: "loja-do-joao"},
		{name: "  Ação & Companhia Ltda. ", want: "acao-companhia-ltda"},
		{name: "ÇÃO", want: "cao"},
		{name: "---", want: ""},
		{name: "mail", want: ""},
		{name: "bounce", want: ""},
		{name: "a very long workspace name that keeps going and going", want: "a-very-long-workspace-name-tha"},
		{name: "loja 123", want: "loja-123"},
		{name: "日本", want: ""},
	}
	for _, tc := range cases {
		if got := SlugCandidate(tc.name); got != tc.want {
			t.Fatalf("SlugCandidate(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSlugFor(t *testing.T) {
	taken := map[string]bool{"loja": true, "loja-2": true}
	got := SlugFor("Loja", "0b6f2c1e-aaaa", func(s string) bool { return taken[s] })
	if got != "loja-3" {
		t.Fatalf("SlugFor = %q, want loja-3", got)
	}
	got = SlugFor("日本", "0b6f2c1e-aaaa", func(string) bool { return false })
	if got != "ws-0b6f2c1e" {
		t.Fatalf("SlugFor fallback = %q, want ws-0b6f2c1e", got)
	}
}

func TestSharedSubdomain(t *testing.T) {
	got, err := SharedSubdomain("loja-do-joao", "vozkomail.com.br")
	if err != nil || got != "loja-do-joao.vozkomail.com.br" {
		t.Fatalf("SharedSubdomain = %q, %v", got, err)
	}
	if _, err := SharedSubdomain("", "vozkomail.com.br"); err == nil {
		t.Fatal("empty slug must fail")
	}
	if _, err := SharedSubdomain("loja", ""); err == nil {
		t.Fatal("empty shared domain must fail")
	}
	if _, err := SharedSubdomain("lo ja", "vozkomail.com.br"); err == nil {
		t.Fatal("invalid slug must fail")
	}
}

func TestAChosenSlugMustAlreadyBeCanonicalAndFree(t *testing.T) {
	cases := []struct {
		slug string
		want error
	}{
		{"cartaodetodososasco", nil},
		{"loja-do-joao-2", nil},
		{"ws-0b6f2c1e", nil},
		{"", ErrInvalidSlug},
		{"Loja", ErrInvalidSlug},
		{"loja do joao", ErrInvalidSlug},
		{"-loja", ErrInvalidSlug},
		{"loja-", ErrInvalidSlug},
		{"loja--joao", ErrInvalidSlug},
		{"joão", ErrInvalidSlug},
		{"a-very-long-workspace-name-that", ErrInvalidSlug},
		{"mail", ErrSlugReserved},
		{"postmaster", ErrSlugReserved},
	}
	for _, tc := range cases {
		if err := ValidateChosenSlug(tc.slug); err != tc.want {
			t.Errorf("ValidateChosenSlug(%q) = %v, want %v", tc.slug, err, tc.want)
		}
	}
}
