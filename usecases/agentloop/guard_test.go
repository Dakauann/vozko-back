package agentloop

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/ai"
)

var errBroke = errors.New("saldo esgotado")

type guardedDriver struct {
	*fakeDriver
	admits int
	failAt int
}

func (g *guardedDriver) Admit(context.Context) error {
	g.admits++
	if g.admits == g.failAt {
		return errBroke
	}
	return nil
}

func runGuarded(prov *fakeAI, drv Driver) (Outcome, *capture) {
	cp := &capture{}
	e := Engine{AI: prov}
	out := e.Run(context.Background(), cp.emit, drv, Config{FinishToolName: "finish"}, &Session{}, "faça X")
	return out, cp
}

func TestRun_GuardIsAskedBeforeEveryModelCall(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}}, {tcs: []ai.ToolCall{tcall("read")}}, {tokens: []string{"pronto"}}}}
	drv := &guardedDriver{fakeDriver: &fakeDriver{}}
	out, _ := runGuarded(prov, drv)
	if out.Kind != OutcomeIdle {
		t.Fatalf("outcome = %+v", out)
	}
	if drv.admits != 3 || len(prov.models) != 3 {
		t.Fatalf("admits = %d model calls = %d, want one admission per call", drv.admits, len(prov.models))
	}
}

func TestRun_GuardStopsTheLoopBeforeTheCallItRefuses(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}}, {tcs: []ai.ToolCall{tcall("read")}}, {tokens: []string{"pronto"}}}}
	drv := &guardedDriver{fakeDriver: &fakeDriver{}, failAt: 2}
	out, cp := runGuarded(prov, drv)
	// The refused call must never reach the provider: that call is the money the gate exists to protect.
	if len(prov.models) != 1 {
		t.Fatalf("model calls = %d, want only the one admitted", len(prov.models))
	}
	if out.Kind != OutcomeDone || out.Valid || !errors.Is(out.Halt, errBroke) {
		t.Fatalf("outcome = %+v, want a halt carrying the guard's reason", out)
	}
	if cp.count(EventIteration) != 1 {
		t.Fatalf("iterations announced = %d, want the refused one never announced", cp.count(EventIteration))
	}
}

func TestRun_GuardRefusingTheFirstCallSpendsNothing(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tokens: []string{"oi"}}}}
	drv := &guardedDriver{fakeDriver: &fakeDriver{}, failAt: 1}
	out, _ := runGuarded(prov, drv)
	if len(prov.models) != 0 || !errors.Is(out.Halt, errBroke) {
		t.Fatalf("model calls = %d outcome = %+v", len(prov.models), out)
	}
}

func TestRun_DriversWithoutAGuardAreUnchanged(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tokens: []string{"oi"}}}}
	out, _ := runGuarded(prov, &fakeDriver{})
	if out.Kind != OutcomeIdle || out.Halt != nil {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestRun_TokenBudgetStopSaysWhy(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}, usage: &ai.Usage{TotalTokens: 100}}}}
	out, _, _ := run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish", SessionTokenBudget: 50})
	// A caller that only reads Summary cannot tell "done" from "cut off"; the halt says it was cut off.
	if !errors.Is(out.Halt, ErrSessionBudget) {
		t.Fatalf("halt = %v, want ErrSessionBudget", out.Halt)
	}
}
