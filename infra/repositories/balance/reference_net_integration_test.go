package balance_repository

import "testing"

func TestTheNetCreditUnderAReferenceIsCreditsMinusDebitsForThatExactReference(t *testing.T) {
	db := sendCapIntegrationDB(t)
	ws := seedWorkspace(t, db, "loja", 0)
	other := seedWorkspace(t, db, "outra", 0)
	record(t, db, ws, "credit", "inv-1", 10_000_000, 5_000_000, false)
	record(t, db, ws, "debit", "inv-1", 4_000_000, 5_000_000, true)
	record(t, db, ws, "credit", "inv-10", 99_000_000, 5_000_000, false)
	record(t, db, other, "credit", "inv-1", 99_000_000, 5_000_000, false)

	reader := NewReferenceNet(db)
	got, err := reader.NetCreditUnder(ws, "inv-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 6_000_000 {
		t.Fatalf("net credit = %d, want 6000000 (exact reference, this workspace only)", got)
	}
}

func TestAReferenceWithNothingRecordedNetsZero(t *testing.T) {
	db := sendCapIntegrationDB(t)
	ws := seedWorkspace(t, db, "loja", 0)

	got, err := NewReferenceNet(db).NetCreditUnder(ws, "reversal:inv-1")
	if err != nil || got != 0 {
		t.Fatalf("net credit = %d, %v; want 0", got, err)
	}
}
