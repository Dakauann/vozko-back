package copilot_usecase

import (
	"context"
	"errors"
	"testing"
	"vozko/domain/copilot"

	"vozko/domain/ai"
	"vozko/usecases/agentloop"
)

type openFunds struct{}

func (openFunds) Check(string) error { return nil }

type drainingFunds struct {
	checks  int
	allowed int
}

func (d *drainingFunds) Check(string) error {
	d.checks++
	if d.checks > d.allowed {
		return errors.New("aichat: insufficient balance")
	}
	return nil
}

type events struct{ byType map[string][]interface{} }

func (e *events) emit(t string, p interface{}) {
	if e.byType == nil {
		e.byType = map[string][]interface{}{}
	}
	e.byType[t] = append(e.byType[t], p)
}

func TestService_BalanceIsCheckedBeforeEveryModelCall(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	rt := &fakeTool{name: "read_x", meta: readMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("read_x", nil)}, {call("read_x", nil)}, {}}, texts: []string{"Vou consultar.", "", "fim"}}
	funds := &drainingFunds{allowed: 1}
	svc := NewService(agentloop.Engine{AI: prov}, NewRegistry(rt), &fakeAccess{}, funds, th, ms, nil, nil, func() string { return "a" })
	ev := &events{}
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "x"}, ownerCtx, ev.emit); err != nil {
		t.Fatal(err)
	}
	// One admitted call, then the ledger says no: the second call must never be paid for.
	if prov.idx != 1 || funds.checks != 2 {
		t.Fatalf("model calls = %d checks = %d", prov.idx, funds.checks)
	}
	errs := ev.byType["error"]
	if len(errs) != 1 || errs[0].(map[string]interface{})["error"] != msgFundsExhausted {
		t.Fatalf("error events = %v, want the funds message", errs)
	}
	if len(ev.byType["done"]) != 1 {
		t.Fatal("the client must still get done so the answer stops streaming")
	}
	if ms.last().Content != "Vou consultar." {
		t.Fatalf("partial answer = %q, want what was already said kept", ms.last().Content)
	}
}

func TestService_AnAnswerHasATokenBudget(t *testing.T) {
	if cfg := DefaultConfig(ownerCtx, AnswerTokenBudget); cfg.SessionTokenBudget != AnswerTokenBudget || AnswerTokenBudget <= 0 {
		t.Fatalf("budget = %d", cfg.SessionTokenBudget)
	}
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("read_x", nil)}, {}}, texts: []string{"", "fim"}, usage: &ai.Usage{TotalTokens: AnswerTokenBudget}}
	svc := NewService(agentloop.Engine{AI: prov}, NewRegistry(&fakeTool{name: "read_x", meta: readMeta}), &fakeAccess{}, openFunds{}, th, ms, nil, nil, func() string { return "a" })
	ev := &events{}
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "x"}, ownerCtx, ev.emit); err != nil {
		t.Fatal(err)
	}
	errs := ev.byType["error"]
	if prov.idx != 1 || len(errs) != 1 || errs[0].(map[string]interface{})["error"] != msgBudgetExhausted {
		t.Fatalf("model calls = %d errors = %v, want a visible stop after the budget", prov.idx, errs)
	}
}

func (s *scriptAI) usageOrEmpty() *ai.Usage {
	if s.usage == nil {
		return &ai.Usage{}
	}
	return s.usage
}
