package lead

import (
	"errors"
	"strings"
	"testing"
)

func TestTheConsentPurposeIsOptionalAndBounded(t *testing.T) {
	cases := []struct {
		name    string
		purpose string
		want    error
	}{
		{"empty", "", nil},
		{"spaces only", "   ", nil},
		{"at the limit", strings.Repeat("p", MaxConsentPurposeLength), nil},
		{"at the limit once spaces collapse", strings.Repeat("p ", MaxConsentPurposeLength/2), nil},
		{"over the limit", strings.Repeat("p", MaxConsentPurposeLength+1), ErrConsentPurposeTooLong},
	}
	for _, tc := range cases {
		if err := ValidateConsentPurpose(tc.purpose); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if got := NormalizeConsentPurpose("  receber \n ofertas "); got != "receber ofertas" {
		t.Fatalf("normalized = %q", got)
	}
}
