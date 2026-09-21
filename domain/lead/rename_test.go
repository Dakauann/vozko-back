package lead

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateName_AcceptsAnOrdinaryName(t *testing.T) {
	if err := ValidateName("Ana Maria Souza"); err != nil {
		t.Fatalf("ValidateName: %v", err)
	}
}

func TestValidateName_EmptyIsValidBecauseItClearsTheName(t *testing.T) {
	if err := ValidateName(""); err != nil {
		t.Fatalf("empty name must be allowed, it is how a name is cleared: %v", err)
	}
	if err := ValidateName("   "); err != nil {
		t.Fatalf("whitespace-only must clear too: %v", err)
	}
}

func TestValidateName_RejectsSomethingThatWillNotFit(t *testing.T) {
	err := ValidateName(strings.Repeat("a", MaxLeadNameLength+1))
	if !errors.Is(err, ErrLeadNameTooLong) {
		t.Fatalf("err = %v, want ErrLeadNameTooLong", err)
	}
}

func TestValidateName_LimitCountsRunesNotBytes(t *testing.T) {
	name := strings.Repeat("ç", MaxLeadNameLength)
	if err := ValidateName(name); err != nil {
		t.Fatalf("a %d-rune name must fit: %v", MaxLeadNameLength, err)
	}
}

func TestNormalizeName_TrimsAndCollapsesWhitespace(t *testing.T) {
	cases := map[string]string{
		"  Ana Maria  ":  "Ana Maria",
		"Ana    Maria":   "Ana Maria",
		"\tAna\nMaria\t": "Ana Maria",
		"":               "",
		"    ":           "",
	}
	for input, want := range cases {
		if got := NormalizeName(input); got != want {
			t.Fatalf("NormalizeName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMerge_EmptyNameStillMeansLeaveItAlone(t *testing.T) {
	l := Lead{Name: "Ana Maria"}
	l.Merge(LeadUpdate{Name: ""})

	if l.Name != "Ana Maria" {
		t.Fatalf("Merge cleared the name on an empty update; a webhook with no "+
			"pushname would erase operator-entered names. got %q", l.Name)
	}
}

func TestMerge_NonEmptyNameStillOverwrites(t *testing.T) {
	l := Lead{Name: "Old"}
	l.Merge(LeadUpdate{Name: "New"})
	if l.Name != "New" {
		t.Fatalf("Name = %q, want New", l.Name)
	}
}
