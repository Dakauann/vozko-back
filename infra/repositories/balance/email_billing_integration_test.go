package balance_repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/workspace/workspace_pricing"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
	balance_usecase "vozko/usecases/balance"
)

const productionMaxOpenConns = 10

type emailBillingFixture struct {
	db      *gorm.DB
	billing *balance_usecase.ConsumeEmailUseCase
	unit    int64
}

func newEmailBillingFixture(t *testing.T) *emailBillingFixture {
	t.Helper()
	db := sendCapIntegrationDB(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	pool.SetMaxOpenConns(productionMaxOpenConns)
	pool.SetMaxIdleConns(productionMaxOpenConns)
	billing := balance_usecase.NewConsumeEmailUseCase(NewRepository(db), workspace_pricing.NewPricer(flatPricing{}), activeSubscription{})
	quote, err := billing.QuoteRun(context.Background(), uuid.New().String(), 1)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	return &emailBillingFixture{db: db, billing: billing, unit: quote.UnitMicros}
}

func (f *emailBillingFixture) amount(t *testing.T, workspaceID string) int64 {
	t.Helper()
	var row schema.Balance
	if err := f.db.Where("workspace_id = ?", workspaceID).First(&row).Error; err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return row.Amount
}

func (f *emailBillingFixture) rowsWithReference(t *testing.T, reference string) []schema.BalanceTransaction {
	t.Helper()
	var rows []schema.BalanceTransaction
	if err := f.db.Where("reference_id = ?", reference).Find(&rows).Error; err != nil {
		t.Fatalf("read transactions: %v", err)
	}
	return rows
}

func parallel(n int, work func(i int)) {
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			work(i)
		}(i)
	}
	wg.Wait()
}

func TestEmailBillingIntegration_ConcurrentBatchChargesDebitTheBatchOnce(t *testing.T) {
	f := newEmailBillingFixture(t)
	const units = 1_000
	start := f.unit * units * 3
	ws := seedWorkspace(t, f.db, "Batch", start)
	campaign := uuid.New().String()
	quote, err := f.billing.QuoteRun(context.Background(), ws, units)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}

	var failures atomic.Int64
	parallel(50, func(int) {
		if err := f.billing.ChargeBatch(context.Background(), ws, campaign, 1, quote); err != nil {
			failures.Add(1)
		}
	})

	if failures.Load() != 0 {
		t.Fatalf("%d concurrent charges of one batch failed; a repeat must return quietly", failures.Load())
	}
	if rows := f.rowsWithReference(t, balance_usecase.EmailCampaignBatchReference(campaign, 1)); len(rows) != 1 {
		t.Fatalf("debits for the batch = %d, want exactly 1", len(rows))
	}
	if got, want := f.amount(t, ws), start-f.unit*units; got != want {
		t.Fatalf("balance = %d, want %d", got, want)
	}
	repotest.AssertLedgerChains(t, f.db, ws, start)
}

func TestEmailBillingIntegration_ConcurrentBatchRefundsCreditTheUnsentOnce(t *testing.T) {
	f := newEmailBillingFixture(t)
	const units, unsent = 1_000, 400
	start := f.unit * units * 2
	ws := seedWorkspace(t, f.db, "Refund batch", start)
	campaign := uuid.New().String()
	quote, err := f.billing.QuoteRun(context.Background(), ws, units)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if err := f.billing.ChargeBatch(context.Background(), ws, campaign, 2, quote); err != nil {
		t.Fatalf("charge: %v", err)
	}

	var failures atomic.Int64
	parallel(50, func(int) {
		if err := f.billing.RefundBatch(context.Background(), ws, campaign, 2, quote, unsent); err != nil {
			failures.Add(1)
		}
	})

	if failures.Load() != 0 {
		t.Fatalf("%d concurrent refunds failed", failures.Load())
	}
	refunds := f.rowsWithReference(t, balance_usecase.RefundReference(balance_usecase.EmailCampaignBatchReference(campaign, 2)))
	if len(refunds) != 1 || refunds[0].Amount != f.unit*unsent {
		t.Fatalf("refunds = %+v, want one refund of %d", refunds, f.unit*unsent)
	}
	if got, want := f.amount(t, ws), start-f.unit*(units-unsent); got != want {
		t.Fatalf("balance = %d, want %d (paid exactly the sent emails)", got, want)
	}
	repotest.AssertLedgerChains(t, f.db, ws, start)
}

func TestEmailBillingIntegration_ConcurrentSingleSendsWithOneKeyRecordOnce(t *testing.T) {
	f := newEmailBillingFixture(t)
	start := f.unit * 100
	ws := seedWorkspace(t, f.db, "Single", start)
	key := "tx:" + uuid.New().String()

	var recorded, failures atomic.Int64
	parallel(50, func(int) {
		charge, err := f.billing.ChargeMessageRound(context.Background(), ws, key)
		if err != nil {
			failures.Add(1)
			return
		}
		if charge.Recorded {
			recorded.Add(1)
		}
	})

	if failures.Load() != 0 || recorded.Load() != 1 {
		t.Fatalf("recorded = %d, failures = %d; exactly one caller may own the charge", recorded.Load(), failures.Load())
	}
	if got := f.amount(t, ws); got != start-f.unit {
		t.Fatalf("balance = %d, want %d", got, start-f.unit)
	}
	repotest.AssertLedgerChains(t, f.db, ws, start)
}

func TestEmailBillingIntegration_AShortWalletNeverGoesNegativeUnderLoad(t *testing.T) {
	f := newEmailBillingFixture(t)
	const affordable, attempts = 100, 150
	start := f.unit * affordable
	ws := seedWorkspace(t, f.db, "Short", start)

	var ok, short, other atomic.Int64
	var firstOther atomic.Value
	parallel(attempts, func(i int) {
		_, err := f.billing.ChargeMessage(context.Background(), ws, fmt.Sprintf("short:%d", i))
		var insufficient *balance_usecase.InsufficientEmailBalanceError
		switch {
		case err == nil:
			ok.Add(1)
		case errors.As(err, &insufficient):
			short.Add(1)
		default:
			other.Add(1)
			firstOther.CompareAndSwap(nil, err.Error())
		}
	})

	if ok.Load() != affordable || short.Load() != attempts-affordable || other.Load() != 0 {
		t.Fatalf("ok %d, short %d, other %d (first: %v); want %d paid and %d refused", ok.Load(), short.Load(), other.Load(), firstOther.Load(), affordable, attempts-affordable)
	}
	if got := f.amount(t, ws); got != 0 {
		t.Fatalf("balance = %d, want 0", got)
	}
	repotest.AssertLedgerChains(t, f.db, ws, start)
}

func TestEmailBillingIntegration_HighVolumeKeepsTheLedgerExact(t *testing.T) {
	f := newEmailBillingFixture(t)
	for _, history := range []int{0, 200_000} {
		t.Run(fmt.Sprintf("with %d earlier ledger rows", history), func(t *testing.T) {
			const sends, workers = 3_000, 32
			start := f.unit * sends * 2
			ws := seedWorkspace(t, f.db, "Volume", start)
			if history > 0 {
				seedLedgerHistory(t, f.db, ws, history)
			}
			var failures atomic.Int64
			began := time.Now()
			parallel(workers, func(w int) {
				for i := w; i < sends; i += workers {
					if _, err := f.billing.ChargeMessage(context.Background(), ws, fmt.Sprintf("vol:%d:%d", history, i)); err != nil {
						failures.Add(1)
					}
				}
			})
			elapsed := time.Since(began)

			if failures.Load() != 0 {
				t.Fatalf("%d of %d distinct sends failed", failures.Load(), sends)
			}
			if got, want := f.amount(t, ws), start-f.unit*sends; got != want {
				t.Fatalf("balance = %d, want %d", got, want)
			}
			var count int64
			if err := f.db.Model(&schema.BalanceTransaction{}).Where("workspace_id = ? AND reference_id LIKE ?", ws, "email_message:vol:%").Count(&count).Error; err != nil {
				t.Fatalf("count: %v", err)
			}
			if count != sends {
				t.Fatalf("debit rows = %d, want %d", count, sends)
			}
			t.Logf("%d debits on one workspace with %d workers in %s: %.0f debits per second", sends, workers, elapsed.Round(time.Millisecond), float64(sends)/elapsed.Seconds())
		})
	}
}

func TestEmailBillingIntegration_RefundsNeverExceedWhatWasCharged(t *testing.T) {
	f := newEmailBillingFixture(t)
	start := f.unit * 1_000
	ws := seedWorkspace(t, f.db, "Refund", start)

	t.Run("a refund for a charge that never happened credits nothing", func(t *testing.T) {
		forged := balance_usecase.EmailQuote{Units: 1, UnitMicros: f.unit, UnitCostMicros: 0, TotalMicros: f.unit}
		err := f.billing.RefundMessage(context.Background(), ws, "never-charged", forged)
		if err == nil {
			t.Fatalf("refund without a debit was accepted")
		}
		if got := f.amount(t, ws); got != start {
			t.Fatalf("balance = %d, want %d", got, start)
		}
	})

	t.Run("a refund larger than its batch credits nothing", func(t *testing.T) {
		campaign := uuid.New().String()
		charge, err := f.billing.QuoteRun(context.Background(), ws, 10)
		if err != nil {
			t.Fatalf("quote: %v", err)
		}
		if err := f.billing.ChargeBatch(context.Background(), ws, campaign, 1, charge); err != nil {
			t.Fatalf("charge: %v", err)
		}
		inflated := charge
		inflated.Units = 1_000
		inflated.TotalMicros = inflated.UnitMicros * inflated.Units
		before := f.amount(t, ws)
		if err := f.billing.RefundBatch(context.Background(), ws, campaign, 1, inflated, inflated.Units); err == nil {
			t.Fatalf("a refund of %d units for a batch of %d was accepted", inflated.Units, charge.Units)
		}
		if got := f.amount(t, ws); got != before {
			t.Fatalf("balance moved from %d to %d", before, got)
		}
	})
}

func seedLedgerHistory(t *testing.T, db *gorm.DB, workspaceID string, rows int) {
	t.Helper()
	err := db.Exec(`INSERT INTO balance_transactions (id, balance_id, workspace_id, type, amount, resource_type, balance_before, balance_after, service_type, reference_id, description, created_at)
		SELECT gen_random_uuid(), b.id, b.workspace_id, 'debit', 1, 'money', 0, 0, 'whatsapp_campaign', 'history:' || g, 'history', now() - interval '30 days'
		FROM balances b, generate_series(1, ?) g WHERE b.workspace_id = ?`, rows, workspaceID).Error
	if err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := db.Exec("ANALYZE balance_transactions").Error; err != nil {
		t.Fatalf("analyze: %v", err)
	}
}
