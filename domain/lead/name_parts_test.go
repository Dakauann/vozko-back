package lead

import "testing"

func TestSplitNameUsesTheRealName(t *testing.T) {
	cases := []struct {
		name  string
		lead  Lead
		first string
		rest  string
	}{
		{name: "a full name", lead: Lead{Name: "  Ana   Maria Souza "}, first: "Ana", rest: "Maria Souza"},
		{name: "a single name", lead: Lead{Name: "Ana"}, first: "Ana"},
		{name: "a name that is the lead's own number", lead: Lead{Name: "5584999990001", Number: "5584999990001"}},
		{name: "no name", lead: Lead{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, rest := tc.lead.SplitName()
			if first != tc.first || rest != tc.rest {
				t.Fatalf("SplitName = %q %q, want %q %q", first, rest, tc.first, tc.rest)
			}
		})
	}
}
