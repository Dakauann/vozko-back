package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

func providerBusyFor(wait time.Duration) error {
	return &ai.ProviderBusyError{Reason: "in_flight_budget_exhausted", Limit: "openrouter_in_flight_budget", RetryAfter: wait, Err: errors.New("status 402")}
}

func TestABusyProviderTellsThePersonWhenToTryAgainAndKeepsTheWorkDone(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	rt := &fakeTool{name: "list_agents", meta: readMeta}
	prov := &scriptAI{
		turns:  [][]ai.ToolCall{{call("list_agents", nil)}},
		faults: []error{nil, providerBusyFor(120 * time.Second), nil},
	}
	var failures []map[string]interface{}
	emit := func(eventType string, payload interface{}) {
		if p, ok := payload.(map[string]interface{}); ok && eventType == "error" {
			failures = append(failures, p)
		}
	}
	svc := newService(prov, th, ms, rt)
	svc.retryBase = time.Hour
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "quantos agentes?"}, ownerCtx, emit); err != nil {
		t.Fatalf("stream: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("want one error event, got %v", failures)
	}
	failure := failures[0]
	if failure["code"] != ErrorCodeProviderBusy || failure["retryAfterSeconds"] != 120 {
		t.Fatalf("the error must carry the busy code and the wait, got %v", failure)
	}
	if text, _ := failure["error"].(string); !strings.Contains(text, "2 minutos") {
		t.Fatalf("the fallback text must name the wait, got %q", text)
	}
	if rt.calls != 1 {
		t.Fatalf("the tool that already ran must run once, ran %d times", rt.calls)
	}
	saved := ms.last()
	var steps []toolStep
	if saved == nil || json.Unmarshal(saved.ToolCalls, &steps) != nil || len(steps) != 1 || steps[0].Name != "list_agents" {
		t.Fatalf("the completed work must stay in the conversation, got %+v", saved)
	}
}

func TestBusyHaltsRoundTheWaitUpToWholeMinutes(t *testing.T) {
	cases := []struct {
		wait    time.Duration
		seconds int
		text    string
	}{
		{wait: 120 * time.Second, seconds: 120, text: "cerca de 2 minutos"},
		{wait: 30 * time.Second, seconds: 30, text: "cerca de 1 minuto"},
		{wait: 61 * time.Second, seconds: 61, text: "cerca de 2 minutos"},
		{wait: 0, seconds: 0, text: "alguns minutos"},
	}
	for _, c := range cases {
		halt := fmt.Errorf("%w: %w", agentloop.ErrProviderFailed, providerBusyFor(c.wait))
		failure := haltFailure(halt)
		if failure["code"] != ErrorCodeProviderBusy {
			t.Errorf("wait %v: code = %v", c.wait, failure["code"])
		}
		if seconds, present := failure["retryAfterSeconds"]; (c.seconds == 0 && present) || (c.seconds != 0 && seconds != c.seconds) {
			t.Errorf("wait %v: retryAfterSeconds = %v", c.wait, seconds)
		}
		if text, _ := failure["error"].(string); !strings.Contains(text, c.text) || strings.Contains(text, "402") || strings.Contains(strings.ToLower(text), "interna") {
			t.Errorf("wait %v: text = %q, want it to say %q without technical details", c.wait, text, c.text)
		}
	}
}

func TestOnlyABusyRefusalCarriesAWait(t *testing.T) {
	refused := fmt.Errorf("%w: %w", agentloop.ErrProviderFailed, errors.New("status 402"))
	failure := haltFailure(refused)
	if failure["error"] != msgProviderFailed || failure["code"] != nil || failure["retryAfterSeconds"] != nil {
		t.Fatalf("a plain refusal keeps the internal failure message, got %v", failure)
	}
	if haltFailure(nil) != nil {
		t.Fatal("no halt means no error event")
	}
}
