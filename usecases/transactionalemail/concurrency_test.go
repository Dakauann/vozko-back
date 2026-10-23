package transactionalemail

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	es "vozko/domain/emailsending"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	balance_usecase "vozko/usecases/balance"
)

func TestSendDoesNotSendWhileAnotherCallHoldsTheCharge(t *testing.T) {
	h := newHarness()
	h.billing.heldByOther = true
	_, err := h.service(t).Send(context.Background(), textRequest())
	if codeOf(t, err) != CodeSendInProgress || !errors.Is(err, ErrSendInProgress) {
		t.Fatalf("err = %v", err)
	}
	if len(h.gateway.requests) != 0 || len(h.billing.refunded) != 0 {
		t.Fatalf("a call that did not record the debit must neither send nor refund: events %v", h.log.events)
	}
}

func TestSendReturnsTheMessageTheChargeHolderRecorded(t *testing.T) {
	h := newHarness()
	h.billing.heldByOther = true
	h.runs.later = []es.Message{{ID: "winner", ProviderMessageID: "prov-w", Status: es.MessageSent, Address: "ana@acme.com.br"}}
	res, err := h.service(t).Send(context.Background(), textRequest())
	if err != nil || !res.Repeated || res.MessageID != "winner" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if len(h.gateway.requests) != 0 {
		t.Fatal("the message was already sent by the call that holds the charge")
	}
}

type referenceLedger struct {
	mu      sync.Mutex
	amount  int64
	seen    map[string]bool
	debits  int
	credits int
}

func (l *referenceLedger) DebitBalance(in balance.DebitBalanceInput) (*balance.Transaction, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if in.OncePerReference && l.seen[*in.ReferenceID] {
		return nil, balance.ErrReferenceAlreadyRecorded
	}
	if l.amount < in.Amount {
		return nil, balance.ErrInsufficientBalance
	}
	l.seen[*in.ReferenceID] = true
	l.amount -= in.Amount
	l.debits++
	return &balance.Transaction{Amount: in.Amount}, nil
}

func (l *referenceLedger) CreditBalance(in balance.CreditBalanceInput) (*balance.Transaction, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if in.OncePerReference && l.seen[*in.ReferenceID] {
		return nil, balance.ErrReferenceAlreadyRecorded
	}
	l.seen[*in.ReferenceID] = true
	l.amount += in.Amount
	l.credits++
	return &balance.Transaction{Amount: in.Amount}, nil
}

func (l *referenceLedger) GetByWorkspaceID(string) (*balance.Balance, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return &balance.Balance{Amount: l.amount}, nil
}

func (l *referenceLedger) ExistsTransactionByReferenceID(ref string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[ref], nil
}

type fixedPrice struct{}

func (fixedPrice) PriceEmail(string, workspace_pricing.EmailPriceLine) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{PriceMicros: 583, CostMicros: 165, ProfitMicros: 418}, nil
}

type activeSubscription struct{}

func (activeSubscription) Execute(string) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, nil
}

type sharedOutbox struct {
	mu    sync.Mutex
	rows  []es.Message
	sends int
	err   error
}

func (o *sharedOutbox) Send(_ context.Context, req es.SendRequest, compose es.ComposeFunc) (*es.Message, error) {
	if _, err := compose(es.Envelope{}); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sends++
	if o.err != nil {
		o.rows = append(o.rows, es.Message{ID: "failed", RunKind: req.RunKind, RunID: req.RunID, Status: es.MessageFailed})
		return nil, o.err
	}
	m := es.Message{ID: "msg-1", ProviderMessageID: "prov-1", RunKind: req.RunKind, RunID: req.RunID, Address: req.To, Status: es.MessageSent}
	o.rows = append(o.rows, m)
	return &m, nil
}

func (o *sharedOutbox) ListByRun(_ context.Context, _ string, _ es.RunKind, runID string) ([]es.Message, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []es.Message
	for _, m := range o.rows {
		if m.RunID == runID {
			out = append(out, m)
		}
	}
	return out, nil
}

func realLedgerService(t *testing.T, ledger *referenceLedger, outbox *sharedOutbox) *Service {
	t.Helper()
	h := newHarness()
	billing := balance_usecase.NewConsumeEmailUseCase(ledger, fixedPrice{}, activeSubscription{})
	s, err := New(Deps{Gateway: outbox, Billing: billing, Runs: outbox, Designs: h.designs, LeadFacts: h.facts, Leads: h.leads, Transcript: &transcriptFake{}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConcurrentSendsWithTheSameKeySendAndChargeOnce(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		ledger := &referenceLedger{amount: 100_000, seen: map[string]bool{}}
		outbox := &sharedOutbox{}
		s := realLedgerService(t, ledger, outbox)
		const callers = 2
		results := make([]Result, callers)
		errs := make([]error, callers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				results[i], errs[i] = s.Send(context.Background(), textRequest())
			}(i)
		}
		close(start)
		wg.Wait()
		if outbox.sends != 1 || ledger.debits != 1 || ledger.credits != 0 || ledger.amount != 100_000-583 {
			t.Fatalf("attempt %d: sends %d, debits %d, credits %d, amount %d", attempt, outbox.sends, ledger.debits, ledger.credits, ledger.amount)
		}
		fresh := 0
		for i := range results {
			switch {
			case errs[i] == nil && !results[i].Repeated:
				fresh++
			case errs[i] == nil && results[i].Repeated && results[i].MessageID == "msg-1":
			case errs[i] != nil && CodeOf(errs[i]) == CodeSendInProgress:
			default:
				t.Fatalf("attempt %d: caller %d got %+v, %v", attempt, i, results[i], errs[i])
			}
		}
		if fresh != 1 {
			t.Fatalf("attempt %d: exactly one caller sends, got %d", attempt, fresh)
		}
	}
}

func TestRetryAfterARefundedFailureChargesANewRound(t *testing.T) {
	ledger := &referenceLedger{amount: 100_000, seen: map[string]bool{}}
	outbox := &sharedOutbox{err: errors.New("provider down")}
	s := realLedgerService(t, ledger, outbox)
	if _, err := s.Send(context.Background(), textRequest()); CodeOf(err) != CodeSendFailed {
		t.Fatalf("first = %v", err)
	}
	if ledger.debits != 1 || ledger.credits != 1 || ledger.amount != 100_000 {
		t.Fatalf("the failed send is refunded: debits %d, credits %d, amount %d", ledger.debits, ledger.credits, ledger.amount)
	}
	outbox.err = nil
	res, err := s.Send(context.Background(), textRequest())
	if err != nil || res.Repeated || res.MessageID != "msg-1" {
		t.Fatalf("retry = %+v, %v", res, err)
	}
	if outbox.sends != 2 || ledger.debits != 2 || ledger.amount != 100_000-583 {
		t.Fatalf("the retry pays its own round: sends %d, debits %d, amount %d", outbox.sends, ledger.debits, ledger.amount)
	}
	again, err := s.Send(context.Background(), textRequest())
	if err != nil || !again.Repeated || outbox.sends != 2 || ledger.debits != 2 {
		t.Fatalf("a third call repeats: %+v, %v, sends %d, debits %d", again, err, outbox.sends, ledger.debits)
	}
}

func TestRetryAfterAThrottledSendSendsOnceAndChargesOnce(t *testing.T) {
	ledger := &referenceLedger{amount: 100_000, seen: map[string]bool{}}
	outbox := &sharedOutbox{err: &es.SendError{Kind: es.SendErrorTransient, Detail: "pacing: rate", RetryAfter: time.Second}}
	s := realLedgerService(t, ledger, outbox)
	if _, err := s.Send(context.Background(), textRequest()); CodeOf(err) != CodeSendThrottled {
		t.Fatalf("first = %v", err)
	}
	if ledger.amount != 100_000 {
		t.Fatalf("a throttled send is refunded: amount %d", ledger.amount)
	}
	outbox.err = nil
	for i := 0; i < 2; i++ {
		if _, err := s.Send(context.Background(), textRequest()); err != nil {
			t.Fatalf("retry %d = %v", i, err)
		}
	}
	if outbox.sends != 2 || ledger.amount != 100_000-583 {
		t.Fatalf("the retries send once and pay once: sends %d (the throttled attempt counts one), amount %d", outbox.sends, ledger.amount)
	}
}
