package shared

import (
	"strings"
	"testing"
)

func TestIdentifierNamesAreLowercaseDigitsAndUnderscoreUpTo64(t *testing.T) {
	cases := map[string]bool{
		"cpf":                   true,
		"unifecaf_access_token": true,
		"a1_b2":                 true,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
		"":                      false,
		"CPF":                   false,
		"com-hifen":             false,
		"com espaço":            false,
		"acentuação":            false,
	}
	for name, want := range cases {
		if got := ValidIdentifierName(name); got != want {
			t.Errorf("ValidIdentifierName(%q) = %v, want %v", name, got, want)
		}
	}
}
