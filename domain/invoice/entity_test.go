package invoice

import "testing"

func TestPurpose_Valid(t *testing.T) {
	for _, p := range []Purpose{PurposeTopUp, PurposeSubscription, PurposeMonthlyBilling} {
		if !p.Valid() {
			t.Errorf("%q should be a valid purpose", p)
		}
	}
	if Purpose("BOGUS").Valid() {
		t.Error("BOGUS should be invalid")
	}
	// Validity is checked after normalization (case + whitespace).
	if !Purpose("  monthly_billing ").Valid() {
		t.Error("normalized monthly_billing should be valid")
	}
}

func TestPurpose_NormalizeDefaultsToTopUp(t *testing.T) {
	if Purpose("").Normalize() != PurposeTopUp {
		t.Errorf("empty purpose should normalize to TOP_UP, got %q", Purpose("").Normalize())
	}
	if Purpose(" monthly_billing ").Normalize() != PurposeMonthlyBilling {
		t.Errorf("expected MONTHLY_BILLING, got %q", Purpose(" monthly_billing ").Normalize())
	}
}
