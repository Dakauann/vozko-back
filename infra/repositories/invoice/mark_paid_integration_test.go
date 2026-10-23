package invoice_repository

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/invoice"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestARefundedInvoiceIsNeverPaidAgainByAReplayedPayment(t *testing.T) {
	db := repotest.IsolatedDB(t, "invoice_mark_paid", &schema.Invoice{})
	repo := NewRepository(db)
	seed := func(status invoice.Status) string {
		id := uuid.NewString()
		row := &schema.Invoice{ID: id, WorkspaceID: uuid.NewString(), UserID: uuid.NewString(), AmountBRL: 10, ExchangeRate: 5,
			Status: string(status), BillingType: "PIX", ExternalID: ptr(uuid.NewString())}
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}

	for status, want := range map[invoice.Status]bool{
		invoice.StatusPending:  true,
		invoice.StatusOverdue:  true,
		invoice.StatusExpired:  true,
		invoice.StatusPaid:     false,
		invoice.StatusRefunded: false,
		invoice.StatusReversed: false,
	} {
		transitioned, err := repo.MarkPaid(seed(status), 2_000_000)
		if err != nil || transitioned != want {
			t.Errorf("%s: transitioned = %v, %v; want %v", status, transitioned, err, want)
		}
	}
}

func ptr(value string) *string { return &value }
