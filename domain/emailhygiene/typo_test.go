package emailhygiene

import "testing"

func TestSuggestFixesCommonTyposOfBigAndBrazilianMailboxes(t *testing.T) {
	cases := []struct {
		domain string
		want   string
	}{
		{domain: "gmial.com", want: "gmail.com"},
		{domain: "gamil.com", want: "gmail.com"},
		{domain: "gmail.con", want: "gmail.com"},
		{domain: "gmail.co", want: "gmail.com"},
		{domain: "gmaill.com", want: "gmail.com"},
		{domain: "gnail.com", want: "gmail.com"},
		{domain: "hotmial.com", want: "hotmail.com"},
		{domain: "hotmail.com.bt", want: "hotmail.com.br"},
		{domain: "hotmal.com", want: "hotmail.com"},
		{domain: "outlok.com", want: "outlook.com"},
		{domain: "outllok.com", want: "outlook.com"},
		{domain: "yaho.com.br", want: "yahoo.com.br"},
		{domain: "yahoo.com.vr", want: "yahoo.com.br"},
		{domain: "uol.com.bt", want: "uol.com.br"},
		{domain: "uol.con.br", want: "uol.com.br"},
		{domain: "bol.com.r", want: "bol.com.br"},
		{domain: "terra.com.bt", want: "terra.com.br"},
		{domain: "tera.com.br", want: "terra.com.br"},
		{domain: "ig.com.bt", want: "ig.com.br"},
		{domain: "globo.con", want: "globo.com"},
		{domain: "glob.com", want: "globo.com"},
		{domain: "icloud.co", want: "icloud.com"},
		{domain: "GMIAL.COM", want: "gmail.com"},
	}
	for _, tc := range cases {
		got, ok := DefaultTypos().Suggest(tc.domain)
		if !ok || got != tc.want {
			t.Fatalf("Suggest(%q) = %q, %v, want %q", tc.domain, got, ok, tc.want)
		}
	}
}

func TestSuggestStaysQuietWhenItCouldBeWrong(t *testing.T) {
	cases := []struct {
		name   string
		domain string
	}{
		{name: "a known mailbox is never a typo", domain: "gmail.com"},
		{name: "ymail is a real provider one edit away from gmail", domain: "ymail.com"},
		{name: "email.com is a real provider one edit away from gmail", domain: "email.com"},
		{name: "mail.com is a real provider one edit away from gmail", domain: "mail.com"},
		{name: "a short name is never rewritten in its name", domain: "sol.com.br"},
		{name: "a short name typo in the name is too ambiguous", domain: "iol.com.br"},
		{name: "a company domain far from every target", domain: "loja.com.br"},
		{name: "two edits away is not a typo we are sure of", domain: "gmali.co"},
		{name: "hotmail in another country is real", domain: "hotmail.fr"},
		{name: "empty input", domain: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := DefaultTypos().Suggest(tc.domain); ok {
				t.Fatalf("Suggest(%q) = %q, want no suggestion", tc.domain, got)
			}
		})
	}
}

func TestTypoIndexDropsVariantsSharedByTwoTargets(t *testing.T) {
	index := NewTypoIndex([]string{"abcde.com", "abcdf.com"}, nil)
	if got, ok := index.Suggest("abcdg.com"); ok {
		t.Fatalf("a variant one edit from two targets must not pick one, got %q", got)
	}
	if got, ok := index.Suggest("abcdee.com"); !ok || got != "abcde.com" {
		t.Fatalf("a variant of one target is still suggested, got %q %v", got, ok)
	}
}

func TestTypoIndexNeverSuggestsForAKnownRealDomain(t *testing.T) {
	index := NewTypoIndex([]string{"gmail.com"}, []string{"gmail.co"})
	if _, ok := index.Suggest("gmail.co"); ok {
		t.Fatal("a real domain listed as such is never a typo")
	}
}
