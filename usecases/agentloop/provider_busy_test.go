package agentloop

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/ai"
)

var providerAtItsLimit = &ai.ProviderBusyError{Reason: "in_flight_budget_exhausted", Limit: "openrouter_in_flight_budget", RetryAfter: 2 * time.Minute, Err: errors.New("status 402")}

func TestABusyProviderHaltsTheTurnAtOnceWithoutWaitingInsideIt(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{{ID: "c1", Name: "generate"}}},
		{openErr: providerAtItsLimit},
		{tokens: []string{"Pronto."}},
	}}
	drv := &fakeDriver{}

	done := make(chan Outcome, 1)
	go func() {
		out, _, _ := run(t, prov, drv, Config{ProviderRetryBase: time.Hour})
		done <- out
	}()
	var out Outcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a busy provider must never be waited out inside the turn")
	}

	if len(prov.inputs) != 2 {
		t.Fatalf("a busy provider is never called again in the same turn, got %d calls", len(prov.inputs))
	}
	if !errors.Is(out.Halt, ErrProviderFailed) || !errors.Is(out.Halt, ai.ErrProviderBusy) || errors.Is(out.Halt, ErrProviderUnreachable) || out.Valid {
		t.Fatalf("the turn must halt as a busy provider, got %+v", out)
	}
	if wait, ok := ai.BusyRetryAfter(out.Halt); !ok || wait != 2*time.Minute {
		t.Fatalf("the halt must carry the provider's wait, got %v %v", wait, ok)
	}
	if !reflect.DeepEqual(drv.dispatched, []string{"generate"}) {
		t.Fatalf("the work done before the refusal stays done, dispatched %v", drv.dispatched)
	}
}

func TestABusyProviderSendsNoRetryNotice(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{openErr: providerAtItsLimit}, {tokens: []string{"Oi."}}}}
	cp := &capture{}
	(&Engine{AI: prov}).Run(t.Context(), cp.emit, &fakeDriver{}, quickRetries(Config{}), &Session{}, "x")
	if cp.count(EventAssistantRetry) != 0 {
		t.Fatal("a busy provider is not a passing fault and must not announce a retry")
	}
}
