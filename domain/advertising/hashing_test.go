package advertising

import "testing"

func TestNormalizationFollowsMetaMatchingRules(t *testing.T) {
	cases := []struct {
		key  MatchKey
		raw  string
		want string
	}{
		{MatchEmail, "  Maria.Silva@Example.COM ", "maria.silva@example.com"},
		{MatchEmail, "not-an-email", ""},
		{MatchPhone, "(11) 98888-7777", "5511988887777"},
		{MatchPhone, "+55 11 98888-7777", "5511988887777"},
		{MatchPhone, "1234", ""},
		{MatchFirstName, " José ", "josé"},
		{MatchLastName, "D'Ávila", "dávila"},
		{MatchCity, "São Paulo", "sãopaulo"},
		{MatchZip, "01310-100", "01310100"},
		{MatchCountry, "BR", "br"},
		{MatchCountry, "Brasil", ""},
		{MatchGender, "Feminino", "f"},
		{MatchBirthYear, "1990", "1990"},
	}
	for _, c := range cases {
		if got := NormalizeMatch(c.key, c.raw, "55"); got != c.want {
			t.Fatalf("%s %q = %q, want %q", c.key, c.raw, got, c.want)
		}
	}
}

func TestHashIsSHA256OfTheNormalizedValue(t *testing.T) {
	if got := HashMatch(MatchEmail, " A@B.co ", ""); got != SHA256Hex("a@b.co") || len(got) != 64 {
		t.Fatalf("hash %q", got)
	}
	if HashMatch(MatchEmail, "bad", "") != "" {
		t.Fatal("invalid value hashed")
	}
}

func TestCustomersWithoutAnIdentifyingFieldAreSkipped(t *testing.T) {
	keys := []MatchKey{MatchPhone, MatchEmail, MatchCountry}
	out := HashCustomers(keys, []Customer{
		{MatchPhone: "11988887777", MatchCountry: "BR"},
		{MatchCountry: "BR"},
		{MatchEmail: "x@y.com"},
	}, "55")
	if len(out.Rows) != 2 || out.Skipped != 1 || out.Rows[1][0] != "" || out.Rows[1][1] == "" {
		t.Fatalf("hashed %+v", out)
	}
}
