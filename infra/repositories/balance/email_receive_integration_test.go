package balance_repository

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"vozko/domain/emailsending"
	balance_usecase "vozko/usecases/balance"
)

func TestEmailReceiveIntegration_ConcurrentDeliveriesChargeOnceIntoTheNegative(t *testing.T) {
	f := newEmailBillingFixture(t)
	const start = 1_000
	ws := seedWorkspace(t, f.db, "Receive", start)
	size := 3*emailsending.InboundChunkBytes + 10
	want := int64(583 + 3*315)

	results := make([]balance_usecase.EmailReceiveCharge, 50)
	var failures, charged atomic.Int64
	parallel(len(results), func(i int) {
		got, err := f.billing.ChargeReceive(context.Background(), ws, "ses-msg-1", size)
		if err != nil {
			failures.Add(1)
			return
		}
		if got.Charged {
			charged.Add(1)
		}
		results[i] = got
	})
	if failures.Load() != 0 || charged.Load() != 1 {
		t.Fatalf("failures %d, charged %d; want one charge and no failure", failures.Load(), charged.Load())
	}
	rows := f.rowsWithReference(t, balance_usecase.EmailReceiveReference("ses-msg-1"))
	if len(rows) != 1 || rows[0].Amount != want || rows[0].Type != "debit" || rows[0].BalanceAfter != start-want {
		t.Fatalf("rows = %+v", rows)
	}
	if got := f.amount(t, ws); got != start-want {
		t.Fatalf("balance = %d, want %d", got, start-want)
	}
	for i, r := range results {
		if r.BalanceAfterMicros != start-want || !emailsending.InboundPaused(r.BalanceAfterMicros) {
			t.Fatalf("delivery %d saw balance %d; every delivery must see the wallet below zero and pause", i, r.BalanceAfterMicros)
		}
	}
}

func TestEmailReceiveIntegration_DistinctMessagesAllChargeAndKeepTheLedgerExact(t *testing.T) {
	f := newEmailBillingFixture(t)
	const start, messages = 5_000, 40
	ws := seedWorkspace(t, f.db, "Burst", start)
	var paused atomic.Int64
	parallel(messages, func(i int) {
		got, err := f.billing.ChargeReceive(context.Background(), ws, fmt.Sprintf("ses-burst-%d", i), 0)
		if err != nil {
			t.Error(err)
			return
		}
		if emailsending.InboundPaused(got.BalanceAfterMicros) {
			paused.Add(1)
		}
	})
	wantEnd := int64(start - messages*583)
	if got := f.amount(t, ws); got != wantEnd {
		t.Fatalf("balance = %d, want %d", got, wantEnd)
	}
	crossedAt := int64(start/583 + 1)
	if paused.Load() != messages-crossedAt+1 {
		t.Fatalf("paused deliveries = %d, want %d: exactly the ones after the wallet crossed zero", paused.Load(), messages-crossedAt+1)
	}
	var debits int64
	if err := f.db.Raw("SELECT COUNT(*) FROM balance_transactions WHERE workspace_id = ? AND reference_id LIKE 'email_receive:%'", ws).Scan(&debits).Error; err != nil {
		t.Fatal(err)
	}
	if debits != messages {
		t.Fatalf("debits = %d, want %d", debits, messages)
	}
}
