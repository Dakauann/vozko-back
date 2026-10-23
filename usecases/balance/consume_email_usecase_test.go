package balance_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/balance"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type emailLedgerFake struct {
	amount    int64
	noBalance bool
	readErr   error
	debits    []balance.DebitBalanceInput
	credits   []balance.CreditBalanceInput
	seen      map[string]bool
	lookErr   error
}

func newEmailLedger(amount int64) *emailLedgerFake {
	return &emailLedgerFake{amount: amount, seen: map[string]bool{}}
}

func (f *emailLedgerFake) DebitBalance(in balance.DebitBalanceInput) (*balance.Transaction, error) {
	if f.noBalance {
		return nil, balance.ErrBalanceNotFound
	}
	if in.OncePerReference && f.seen[*in.ReferenceID] {
		return nil, balance.ErrReferenceAlreadyRecorded
	}
	if !in.AllowNegative && f.amount < in.Amount {
		return nil, balance.ErrInsufficientBalance
	}
	f.seen[*in.ReferenceID] = true
	f.amount -= in.Amount
	f.debits = append(f.debits, in)
	return &balance.Transaction{Amount: in.Amount, BalanceAfter: f.amount}, nil
}

func (f *emailLedgerFake) CreditBalance(in balance.CreditBalanceInput) (*balance.Transaction, error) {
	if in.OncePerReference && f.seen[*in.ReferenceID] {
		return nil, balance.ErrReferenceAlreadyRecorded
	}
	f.seen[*in.ReferenceID] = true
	f.amount += in.Amount
	f.credits = append(f.credits, in)
	return &balance.Transaction{Amount: in.Amount, BalanceAfter: f.amount}, nil
}

func (f *emailLedgerFake) ExistsTransactionByReferenceID(ref string) (bool, error) {
	if f.lookErr != nil {
		return false, f.lookErr
	}
	return f.seen[ref], nil
}

func (f *emailLedgerFake) GetByWorkspaceID(string) (*balance.Balance, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.noBalance {
		return nil, balance.ErrBalanceNotFound
	}
	return &balance.Balance{Amount: f.amount}, nil
}

type emailPricerFake struct {
	prices map[workspace_pricing.EmailPriceLine]workspace_pricing.PriceResult
	err    error
}

func (f emailPricerFake) PriceEmail(_ string, line workspace_pricing.EmailPriceLine) (workspace_pricing.PriceResult, error) {
	if f.err != nil {
		return workspace_pricing.PriceResult{}, f.err
	}
	p, ok := f.prices[line]
	if !ok {
		return workspace_pricing.PriceResult{}, workspace_pricing.ErrPricingItemNotFound
	}
	return p, nil
}

type emailSubscriptionFake struct{ err error }

func (f emailSubscriptionFake) Execute(string) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, f.err
}

func emailPrices() emailPricerFake {
	return emailPricerFake{prices: map[workspace_pricing.EmailPriceLine]workspace_pricing.PriceResult{
		workspace_pricing.EmailSendPerMessage:    {PriceMicros: 583, CostMicros: 165, ProfitMicros: 418},
		workspace_pricing.EmailSendPerChunk:      {PriceMicros: 102, CostMicros: 29, ProfitMicros: 73},
		workspace_pricing.EmailReceivePerMessage: {PriceMicros: 583, CostMicros: 190, ProfitMicros: 393},
		workspace_pricing.EmailReceivePerChunk:   {PriceMicros: 315, CostMicros: 90, ProfitMicros: 225},
	}}
}

func newConsumeEmail(ledger *emailLedgerFake, pricer emailPricerFake, sub emailSubscriptionFake) *ConsumeEmailUseCase {
	return NewConsumeEmailUseCase(ledger, pricer, sub)
}

var emailCtx = context.Background()

func TestQuoteRun(t *testing.T) {
	uc := newConsumeEmail(newEmailLedger(0), emailPrices(), emailSubscriptionFake{})
	q, err := uc.QuoteRun(emailCtx, "ws-1", 1000)
	if err != nil || q.UnitMicros != 583 || q.UnitCostMicros != 165 || q.TotalMicros != 583_000 || q.Units != 1000 {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	cases := []struct {
		name string
		uc   *ConsumeEmailUseCase
		n    int64
		want error
	}{
		{"no recipients", uc, 0, ErrEmailUnitsInvalid},
		{"negative recipients", uc, -3, ErrEmailUnitsInvalid},
		{"price unavailable", newConsumeEmail(newEmailLedger(0), emailPricerFake{}, emailSubscriptionFake{}), 10, balance.ErrPriceUnavailable},
		{"zero price", newConsumeEmail(newEmailLedger(0), emailPricerFake{prices: map[workspace_pricing.EmailPriceLine]workspace_pricing.PriceResult{workspace_pricing.EmailSendPerMessage: {}}}, emailSubscriptionFake{}), 10, balance.ErrPriceUnavailable},
		{"no subscription", newConsumeEmail(newEmailLedger(0), emailPrices(), emailSubscriptionFake{err: workspace_plan.ErrSubscriptionNotCurrent}), 10, workspace_plan.ErrSubscriptionNotCurrent},
	}
	for _, tc := range cases {
		if _, err := tc.uc.QuoteRun(emailCtx, "ws-1", tc.n); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

var batchQuote = EmailQuote{Units: 1000, UnitMicros: 583, UnitCostMicros: 165, TotalMicros: 583_000}

func todayPrice(price int64) emailPricerFake {
	return emailPricerFake{prices: map[workspace_pricing.EmailPriceLine]workspace_pricing.PriceResult{workspace_pricing.EmailSendPerMessage: {PriceMicros: price, CostMicros: 1}}}
}

func TestChargeBatchDebitsTheStoredQuoteOncePerBatch(t *testing.T) {
	ledger := newEmailLedger(1_000_000)
	uc := newConsumeEmail(ledger, todayPrice(999_999), emailSubscriptionFake{})
	if err := uc.ChargeBatch(emailCtx, "ws-1", "camp-1", 2, batchQuote); err != nil {
		t.Fatal(err)
	}
	d := ledger.debits[0]
	if d.Amount != 583_000 || d.CostMicros != 165_000 || d.ProfitMicros != 418_000 || d.ServiceType != balance.ServiceEmail || *d.ReferenceID != "email_campaign:camp-1:batch:2" || !d.OncePerReference || d.AllowNegative {
		t.Fatalf("debit = %+v", d)
	}
	if err := uc.ChargeBatch(emailCtx, "ws-1", "camp-1", 2, batchQuote); err != nil || len(ledger.debits) != 1 || ledger.amount != 417_000 {
		t.Fatalf("a repeated batch is a no-op: %v, debits %d, amount %d", err, len(ledger.debits), ledger.amount)
	}
	if err := uc.ChargeBatch(emailCtx, "ws-1", "camp-1", 3, EmailQuote{Units: 500, UnitMicros: 583, UnitCostMicros: 165, TotalMicros: 291_500}); err != nil || len(ledger.debits) != 2 || *ledger.debits[1].ReferenceID != "email_campaign:camp-1:batch:3" {
		t.Fatalf("the next batch is its own charge: %v, %d", err, len(ledger.debits))
	}
}

func TestChargeBatchRefusesWithTheMissingAmount(t *testing.T) {
	cases := []struct {
		name    string
		ledger  *emailLedgerFake
		missing int64
	}{
		{"short wallet", newEmailLedger(500_000), 83_000},
		{"no wallet yet", &emailLedgerFake{noBalance: true, seen: map[string]bool{}}, 583_000},
		{"unreadable wallet after the refusal", &emailLedgerFake{amount: 10, readErr: errors.New("db down"), seen: map[string]bool{}}, 583_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newConsumeEmail(tc.ledger, emailPrices(), emailSubscriptionFake{}).ChargeBatch(emailCtx, "ws-1", "camp-1", 1, batchQuote)
			var short *InsufficientEmailBalanceError
			if !errors.As(err, &short) || short.MissingMicros != tc.missing || !errors.Is(err, balance.ErrInsufficientBalance) {
				t.Fatalf("err = %v", err)
			}
			if len(tc.ledger.debits) != 0 {
				t.Fatal("nothing may be charged")
			}
		})
	}
}

func TestChargeBatchFailsClosed(t *testing.T) {
	cases := []struct {
		name     string
		campaign string
		batch    int
		quote    EmailQuote
		sub      emailSubscriptionFake
		want     error
	}{
		{"no campaign", "", 1, batchQuote, emailSubscriptionFake{}, balance.ErrReferenceRequired},
		{"no batch number", "camp-1", 0, batchQuote, emailSubscriptionFake{}, ErrEmailUnitsInvalid},
		{"no units", "camp-1", 1, EmailQuote{UnitMicros: 583}, emailSubscriptionFake{}, ErrEmailUnitsInvalid},
		{"no price", "camp-1", 1, EmailQuote{Units: 10}, emailSubscriptionFake{}, balance.ErrPriceUnavailable},
		{"no subscription", "camp-1", 1, batchQuote, emailSubscriptionFake{err: workspace_plan.ErrSubscriptionNotCurrent}, workspace_plan.ErrSubscriptionNotCurrent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := newEmailLedger(1_000_000)
			err := newConsumeEmail(ledger, emailPrices(), tc.sub).ChargeBatch(emailCtx, "ws-1", tc.campaign, tc.batch, tc.quote)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(ledger.debits) != 0 {
				t.Fatal("nothing may be charged")
			}
		})
	}
}

func TestBatchChargedReadsTheLedger(t *testing.T) {
	ledger := newEmailLedger(1_000_000)
	uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{})
	if got, err := uc.BatchCharged(emailCtx, "camp-1", 1); err != nil || got {
		t.Fatalf("before the charge = %v, %v", got, err)
	}
	if err := uc.ChargeBatch(emailCtx, "ws-1", "camp-1", 1, batchQuote); err != nil {
		t.Fatal(err)
	}
	if got, err := uc.BatchCharged(emailCtx, "camp-1", 1); err != nil || !got {
		t.Fatalf("after the charge = %v, %v", got, err)
	}
	ledger.lookErr = errors.New("db down")
	if _, err := uc.BatchCharged(emailCtx, "camp-1", 1); err == nil {
		t.Fatal("an unreadable ledger is an error, never an answer")
	}
}

func TestRefundBatchRefundsAtTheBatchPrice(t *testing.T) {
	cases := []struct {
		name       string
		units      int64
		wantRefund int64
		wantCost   int64
		wantErr    error
	}{
		{"part of the batch", 600, 349_800, 99_000, nil},
		{"the whole batch", 1000, 583_000, 165_000, nil},
		{"nothing to refund", 0, 0, 0, nil},
		{"more than the batch", 1001, 0, 0, ErrEmailUnitsInvalid},
		{"negative", -1, 0, 0, ErrEmailUnitsInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := newEmailLedger(0)
			uc := newConsumeEmail(ledger, todayPrice(999_999), emailSubscriptionFake{})
			err := uc.RefundBatch(emailCtx, "ws-1", "camp-1", 2, batchQuote, tc.units)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v", err)
			}
			if tc.wantRefund == 0 {
				if len(ledger.credits) != 0 {
					t.Fatalf("credits = %+v", ledger.credits)
				}
				return
			}
			c := ledger.credits[0]
			if c.Amount != tc.wantRefund || c.CostMicros != tc.wantCost || !c.IsRefund || *c.ReferenceID != "refund:email_campaign:camp-1:batch:2" || *c.RefundOf != "email_campaign:camp-1:batch:2" || c.ServiceType != balance.ServiceEmail {
				t.Fatalf("refund = %+v", c)
			}
			if err := uc.RefundBatch(emailCtx, "ws-1", "camp-1", 2, batchQuote, tc.units); err != nil || len(ledger.credits) != 1 {
				t.Fatalf("a repeated refund credits once: %v, %d", err, len(ledger.credits))
			}
		})
	}
}

func TestSingleMessageChargeAndRefund(t *testing.T) {
	ledger := newEmailLedger(10_000)
	uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{})
	q, err := uc.ChargeMessage(emailCtx, "ws-1", "msg-1")
	if err != nil || q.TotalMicros != 583 || *ledger.debits[0].ReferenceID != "email_message:msg-1" {
		t.Fatalf("charge = %+v, %v", q, err)
	}
	if _, err := uc.ChargeMessage(emailCtx, "ws-1", "msg-1"); err != nil || len(ledger.debits) != 1 {
		t.Fatalf("repeat = %v, %d", err, len(ledger.debits))
	}
	if err := uc.RefundMessage(emailCtx, "ws-1", "msg-1", q); err != nil {
		t.Fatal(err)
	}
	if err := uc.RefundMessage(emailCtx, "ws-1", "msg-1", q); err != nil || len(ledger.credits) != 1 || ledger.amount != 10_000 {
		t.Fatalf("refund twice = %v, %d, amount %d", err, len(ledger.credits), ledger.amount)
	}
	if err := uc.RefundMessage(emailCtx, "ws-1", "msg-2", EmailQuote{}); !errors.Is(err, balance.ErrPriceUnavailable) {
		t.Fatalf("refund without the charge price = %v", err)
	}
	short := newEmailLedger(100)
	if _, err := newConsumeEmail(short, emailPrices(), emailSubscriptionFake{}).ChargeMessage(emailCtx, "ws-1", "msg-3"); !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Fatalf("short = %v", err)
	}
}

func TestMessageRoundsNeverSendARefundedChargeAgain(t *testing.T) {
	ledger := newEmailLedger(10_000)
	uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{})

	first, err := uc.ChargeMessageRound(emailCtx, "ws-1", "tx:k")
	if err != nil || !first.Recorded || first.Key != "tx:k" || first.Quote.TotalMicros != 583 || len(ledger.debits) != 1 {
		t.Fatalf("first = %+v, %v, debits %d", first, err, len(ledger.debits))
	}
	paid, err := uc.ChargeMessageRound(emailCtx, "ws-1", "tx:k")
	if err != nil || paid.Recorded || paid.Key != "tx:k" || len(ledger.debits) != 1 {
		t.Fatalf("an unrefunded round is already paid: %+v, %v, debits %d", paid, err, len(ledger.debits))
	}
	if err := uc.RefundMessage(emailCtx, "ws-1", first.Key, first.Quote); err != nil {
		t.Fatal(err)
	}
	second, err := uc.ChargeMessageRound(emailCtx, "ws-1", "tx:k")
	if err != nil || !second.Recorded || second.Key != "tx:k#2" || len(ledger.debits) != 2 || *ledger.debits[1].ReferenceID != "email_message:tx:k#2" {
		t.Fatalf("after a refund the next round charges again: %+v, %v, debits %d", second, err, len(ledger.debits))
	}
	if err := uc.RefundMessage(emailCtx, "ws-1", second.Key, second.Quote); err != nil {
		t.Fatal(err)
	}
	third, err := uc.ChargeMessageRound(emailCtx, "ws-1", "tx:k")
	if err != nil || !third.Recorded || third.Key != "tx:k#3" || len(ledger.debits) != 3 || ledger.amount != 10_000-583 {
		t.Fatalf("third = %+v, %v, debits %d, amount %d", third, err, len(ledger.debits), ledger.amount)
	}

	lookFailure := errors.New("db down")
	cases := []struct {
		name string
		uc   *ConsumeEmailUseCase
		key  string
		want error
	}{
		{"no key", uc, " ", balance.ErrReferenceRequired},
		{"short wallet", newConsumeEmail(newEmailLedger(10), emailPrices(), emailSubscriptionFake{}), "tx:a", balance.ErrInsufficientBalance},
		{"unreadable ledger", newConsumeEmail(&emailLedgerFake{seen: map[string]bool{}, amount: 10_000, lookErr: lookFailure}, emailPrices(), emailSubscriptionFake{}), "tx:b", lookFailure},
	}
	for _, tc := range cases {
		if _, err := tc.uc.ChargeMessageRound(emailCtx, "ws-1", tc.key); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}
