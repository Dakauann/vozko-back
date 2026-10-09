package balance_usecase

import (
	"errors"
	"sync"
	"testing"

	"vozko/domain/aiusage"
)

type recordingUsage struct {
	mu      sync.Mutex
	records []aiusage.Record
	err     error
}

func (r *recordingUsage) Record(record aiusage.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, record)
	return nil
}

func (r *recordingUsage) snapshot() []aiusage.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]aiusage.Record(nil), r.records...)
}

func TestAIBilling_RecordsEveryKindOfTokenUnderTheChargeReference(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	sub := newMockSub()
	usage := &recordingUsage{}
	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), nil)
	consumer.SetUsageRecorder(usage)
	_ = consumer.Start()

	event := makeEvent("aichat:th-1:req-1", "ws-1", "gpt-4o-mini", 1000, 500)
	event.CachedTokens = 800
	event.CacheWriteTokens = 100
	event.ReasoningTokens = 50
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.state().acked {
		t.Fatal("expected ACK")
	}
	want := aiusage.Record{ReferenceID: "aichat:th-1:req-1", WorkspaceID: "ws-1", Model: "gpt-4o-mini", Tokens: aiusage.Tokens{Input: 1000, Output: 500, CacheRead: 800, CacheWrite: 100, Reasoning: 50}, Billed: true}
	if got := usage.snapshot(); len(got) != 1 || got[0] != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected the call to be charged once, got %d debits", len(balanceRepo.debits))
	}
}

func TestAIBilling_ChargesFirstAndRetriesUsageWithoutChargingTwice(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	sub := newMockSub()
	usage := &recordingUsage{err: errors.New("usage table unavailable")}
	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), nil)
	consumer.SetUsageRecorder(usage)
	_ = consumer.Start()

	event := makeEvent("aichat:th-1:req-2", "ws-1", "gpt-4o-mini", 1000, 500)
	first := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, first)
	if !first.state().nacked || !first.state().requeued {
		t.Fatal("a usage failure must requeue the event")
	}
	if len(balanceRepo.debits) != 1 {
		t.Fatalf("the charge must not wait on usage, got %d debits", len(balanceRepo.debits))
	}

	usage.mu.Lock()
	usage.err = nil
	usage.mu.Unlock()
	second := &mockAck{deliveryCount: 2}
	fireAndWait(t, sub, event, second)
	if !second.state().acked {
		t.Fatal("expected ACK once usage is recorded")
	}
	if len(balanceRepo.debits) != 1 {
		t.Fatalf("a redelivery must not charge again, got %d debits", len(balanceRepo.debits))
	}
	if len(usage.snapshot()) != 1 {
		t.Fatal("usage must be recorded on the redelivery")
	}
}

func TestAIBilling_RecordsGenerationsWithoutTokens(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	sub := newMockSub()
	usage := &recordingUsage{}
	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), nil)
	consumer.SetUsageRecorder(usage)
	_ = consumer.Start()

	event := makeEvent("aichat:th-1:job-1", "ws-1", "google/imagen", 0, 0)
	event.ProviderCostMicros = 40_000
	fireAndWait(t, sub, event, &mockAck{deliveryCount: 1})

	got := usage.snapshot()
	if len(got) != 1 || got[0].IsModelCall() || !got[0].Billed {
		t.Fatalf("a billed generation leaves a usage row that is not a model call, got %+v", got)
	}
}

func TestAIBilling_RecordsTheTokensOfAnUnpricedCallAsNotBilled(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	sub := newMockSub()
	usage := &recordingUsage{}
	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), nil)
	consumer.SetUsageRecorder(usage)
	_ = consumer.Start()

	fireAndWait(t, sub, makeEvent("aichat:th-1:req-3", "ws-1", "unpriced-model", 1000, 500), &mockAck{deliveryCount: 1})

	got := usage.snapshot()
	if len(got) != 1 || got[0].Billed || got[0].Tokens.Input != 1000 {
		t.Fatalf("an unpriced call still used tokens but was not billed, got %+v", got)
	}
	if len(balanceRepo.debits) != 0 {
		t.Fatalf("nothing to charge, got %d debits", len(balanceRepo.debits))
	}
}
