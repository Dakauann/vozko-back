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
	if !Purpose("  monthly_billing ").Valid() {
		t.Error("normalized monthly_billing should be valid")
	}
}

func TestPurposeNormalizeNeverInventsATopUp(t *testing.T) {
	for _, raw := range []Purpose{"", "   ", "BOGUS"} {
		if raw.Normalize() == PurposeTopUp {
			t.Errorf("%q must not normalize to TOP_UP", raw)
		}
		if raw.Valid() {
			t.Errorf("%q must be invalid", raw)
		}
	}
	if Purpose(" monthly_billing ").Normalize() != PurposeMonthlyBilling {
		t.Errorf("expected MONTHLY_BILLING, got %q", Purpose(" monthly_billing ").Normalize())
	}
}

func TestAnInvoiceWithoutAPurposeHasNoPurpose(t *testing.T) {
	var missing *Invoice
	if missing.NormalizedPurpose() != "" {
		t.Errorf("a nil invoice must not report a purpose, got %q", missing.NormalizedPurpose())
	}
	if (&Invoice{}).NormalizedPurpose() != "" {
		t.Errorf("an empty purpose must stay empty")
	}
}

func TestReversedIsASettledStatus(t *testing.T) {
	settled := map[string]bool{}
	for _, s := range SettledStatuses() {
		settled[s] = true
	}
	for _, s := range []Status{StatusPaid, StatusRefunded, StatusReversed} {
		if !settled[string(s)] {
			t.Errorf("%s must be settled so a late payment event never credits it again", s)
		}
	}
}
