package invoice

import "testing"

func TestTheExternalReferenceRoundTripsTheInvoiceID(t *testing.T) {
	ref := ExternalReference("3f2b")
	id, ok := IDFromExternalReference(ref)
	if !ok || id != "3f2b" {
		t.Fatalf("IDFromExternalReference(%q) = %q, %v", ref, id, ok)
	}
}

func TestForeignExternalReferencesAreNotOurs(t *testing.T) {
	for _, ref := range []string{"", "inv:", "order:12", "  inv:  ", "INV:abc"} {
		if id, ok := IDFromExternalReference(ref); ok {
			t.Errorf("IDFromExternalReference(%q) = %q, want no id", ref, id)
		}
	}
}

func TestReversalTargetIsTheReversedShareOfTheCredit(t *testing.T) {
	cases := []struct {
		name     string
		credited int64
		amount   float64
		reversed float64
		want     int64
	}{
		{"full reversal", 10_000_000, 50, 50, 10_000_000},
		{"half reversal", 10_000_000, 50, 25, 5_000_000},
		{"rounds to the nearest micro", 10_000_000, 30, 10, 3_333_333},
		{"an unknown reversed value takes back everything", 10_000_000, 50, 0, 10_000_000},
		{"a reversed value above the amount is clamped", 10_000_000, 50, 80, 10_000_000},
		{"a broken amount takes back everything", 10_000_000, 0, 25, 10_000_000},
		{"nothing credited means nothing to take back", 0, 50, 50, 0},
		{"a negative credit means nothing to take back", -5, 50, 50, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReversalTargetMicros(tc.credited, tc.amount, tc.reversed); got != tc.want {
				t.Fatalf("ReversalTargetMicros(%d, %v, %v) = %d, want %d", tc.credited, tc.amount, tc.reversed, got, tc.want)
			}
		})
	}
}

func TestReversalMovementsHaveTheirOwnLedgerReference(t *testing.T) {
	if ReversalReference("abc") == "abc" {
		t.Fatal("reversal movements must not share the reference of the original credit")
	}
	if ReversalReference("abc") != ReversalReference("abc") {
		t.Fatal("the reversal reference must be stable for one invoice")
	}
}

func TestAnInvoiceWithoutAChargeIsAReservation(t *testing.T) {
	if !(&Invoice{}).Reserved() {
		t.Fatal("an invoice with no external id is still a reservation")
	}
	if (&Invoice{ExternalID: "pay_1"}).Reserved() {
		t.Fatal("an invoice with an external id has its charge")
	}
}
