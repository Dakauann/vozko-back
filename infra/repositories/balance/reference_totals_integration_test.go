package balance_repository

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/balance"
	"vozko/infra/database/schema"
)

func balanceIDOf(t *testing.T, db *gorm.DB, workspaceID string) string {
	t.Helper()
	var b schema.Balance
	if err := db.Where("workspace_id = ?", workspaceID).First(&b).Error; err != nil {
		t.Fatalf("balance: %v", err)
	}
	return b.ID
}

func record(t *testing.T, db *gorm.DB, workspaceID, txType, reference string, amount, rate int64, refund bool) {
	t.Helper()
	ref := reference
	row := schema.BalanceTransaction{
		ID: uuid.New().String(), BalanceID: balanceIDOf(t, db, workspaceID), WorkspaceID: workspaceID,
		Type: txType, Amount: amount, ResourceType: "money", ServiceType: string(balance.ServiceAI),
		ReferenceID: &ref, ExchangeRateMicros: rate, IsRefund: refund,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
}

func TestTotalsUnderAReferenceAddChargesMinusRefundsAtTheirOwnRate(t *testing.T) {
	db := sendCapIntegrationDB(t)
	ws := seedWorkspace(t, db, "elo", 10_000_000)
	other := seedWorkspace(t, db, "outro", 10_000_000)
	record(t, db, ws, "debit", "aichat:th-1:a", 100_000, 5_500_000, false)
	record(t, db, ws, "debit", "aichat:th-1:b", 20_000, 6_000_000, false)
	record(t, db, ws, "credit", "refund:aichat:th-1:b", 20_000, 6_000_000, true)
	record(t, db, ws, "debit", "aichat:th-10:c", 999_000, 6_000_000, false)
	record(t, db, ws, "debit", "other:th-1:d", 999_000, 6_000_000, false)
	record(t, db, other, "debit", "aichat:th-1:e", 999_000, 6_000_000, false)

	totals, err := NewRepository(db).(balance.ReferenceTotaler).TotalsUnder(ws, "aichat:th-1:")
	if err != nil {
		t.Fatal(err)
	}
	want := balance.ReferenceTotals{LedgerMicros: 100_000, BillingMicros: 550_000, Transactions: 3, Debits: 2}
	if totals != want {
		t.Fatalf("totals = %+v, want %+v", totals, want)
	}
}

func TestTotalsUnderAReferenceFlagAChargeWithoutARate(t *testing.T) {
	db := sendCapIntegrationDB(t)
	ws := seedWorkspace(t, db, "elo", 10_000_000)
	record(t, db, ws, "debit", "aichat:th-1:a", 100_000, 0, false)
	totals, err := NewRepository(db).(balance.ReferenceTotaler).TotalsUnder(ws, "aichat:th-1:")
	if err != nil {
		t.Fatal(err)
	}
	if !totals.MissingRate || totals.Transactions != 1 {
		t.Fatalf("a charge without its rate must be flagged, got %+v", totals)
	}
}

func TestTotalsUnderAReferenceTreatWildcardsLiterally(t *testing.T) {
	db := sendCapIntegrationDB(t)
	ws := seedWorkspace(t, db, "elo", 10_000_000)
	record(t, db, ws, "debit", "aichat:tx1:a", 100_000, 6_000_000, false)
	totals, err := NewRepository(db).(balance.ReferenceTotaler).TotalsUnder(ws, "aichat:t_1:")
	if err != nil {
		t.Fatal(err)
	}
	if totals.Transactions != 0 || totals.BillingMicros != 0 {
		t.Fatalf("an underscore must not match any character, got %+v", totals)
	}
	empty, err := NewRepository(db).(balance.ReferenceTotaler).TotalsUnder(ws, "aichat:none:")
	if err != nil || empty != (balance.ReferenceTotals{}) {
		t.Fatalf("no charges add up to zero, got %+v %v", empty, err)
	}
}
