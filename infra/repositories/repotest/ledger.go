package repotest

import (
	"sort"
	"testing"

	"gorm.io/gorm"

	"vozko/infra/database/schema"
)

func AssertLedgerChains(t testing.TB, db *gorm.DB, workspaceID string, start int64) {
	t.Helper()
	AssertLedgerChainsAllowingDebt(t, db, workspaceID, start, func(schema.BalanceTransaction) bool { return false })
}

func AssertLedgerChainsAllowingDebt(t testing.TB, db *gorm.DB, workspaceID string, start int64, mayOweAfter func(schema.BalanceTransaction) bool) {
	t.Helper()
	var rows []schema.BalanceTransaction
	if err := db.Where("workspace_id = ?", workspaceID).Find(&rows).Error; err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	running := start
	for i, row := range rows {
		if row.BalanceBefore != running {
			t.Fatalf("row %d starts at %d but the balance was %d: a concurrent write was lost", i, row.BalanceBefore, running)
		}
		switch row.Type {
		case "debit":
			running -= row.Amount
		case "credit":
			running += row.Amount
		default:
			t.Fatalf("row %d has type %q", i, row.Type)
		}
		if row.BalanceAfter != running {
			t.Fatalf("row %d ends at %d, want %d", i, row.BalanceAfter, running)
		}
		if running < 0 && !mayOweAfter(row) {
			t.Fatalf("row %d (%s) took the balance negative: %d", i, referenceOf(row), running)
		}
	}
	var balance schema.Balance
	if err := db.Where("workspace_id = ?", workspaceID).First(&balance).Error; err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if balance.Amount != running {
		t.Fatalf("balance row %d disagrees with the ledger %d", balance.Amount, running)
	}
}

func referenceOf(row schema.BalanceTransaction) string {
	if row.ReferenceID == nil {
		return row.Type
	}
	return row.Type + " " + *row.ReferenceID
}
