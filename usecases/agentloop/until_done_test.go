package agentloop

import (
	"fmt"
	"testing"

	"vozko/domain/ai"
)

func TestIteratingUntilDoneRunsPastTheDefaultCap(t *testing.T) {
	var turns []aiTurn
	for i := 0; i < 45; i++ {
		turns = append(turns, aiTurn{tcs: []ai.ToolCall{tcall(fmt.Sprintf("step_%d", i))}})
	}
	turns = append(turns, aiTurn{fullText: "pronto"})
	prov := &fakeAI{turns: turns}
	drv := &fakeDriver{dispatchFn: func(call ai.ToolCall) StepResult {
		return StepResult{Result: "ok", Mutated: true, Signature: call.Name}
	}}
	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", IterateUntilDone: true, NoProgressStop: 100})
	if out.Kind != OutcomeIdle || out.Halt != nil || len(prov.inputs) != 46 {
		t.Fatalf("outcome %+v after %d calls, want the model to decide when to stop", out, len(prov.inputs))
	}
}

func TestUntilDoneUsesTheModelWindowInsteadOfACapWhenTheCatalogKnowsTheModel(t *testing.T) {
	model := ai.ModelInfo{ID: "anthropic/claude-sonnet-4", ContextLength: 200000, PromptPrice: 0.000003}
	c := UntilDone(Config{MaxIterations: 30, SessionTokenBudget: 1_000_000, MaxHistoryMsgs: 80}, model, 250_000)
	if !c.IterateUntilDone || c.MaxIterations != 0 {
		t.Fatalf("iterate=%v max=%d, want no step cap", c.IterateUntilDone, c.MaxIterations)
	}
	if c.ModelLimits.ID != model.ID || c.SessionTokenBudget != 0 || c.MaxHistoryMsgs != 0 {
		t.Fatalf("limits=%q budget=%d history=%d, want the model window with compaction and no token budget or row trim", c.ModelLimits.ID, c.SessionTokenBudget, c.MaxHistoryMsgs)
	}
}

func TestUntilDoneKeepsATokenBudgetOnlyForModelsTheCatalogDoesNotKnow(t *testing.T) {
	c := UntilDone(Config{MaxIterations: 30}, ai.ModelInfo{ID: "unknown/model"}, 250_000)
	if !c.IterateUntilDone || c.MaxIterations != 0 {
		t.Fatalf("iterate=%v max=%d, want no step cap", c.IterateUntilDone, c.MaxIterations)
	}
	if c.SessionTokenBudget != 250_000 || c.ModelLimits.HasKnownLimits() {
		t.Fatalf("budget=%d, want the fallback budget for an uncataloged model", c.SessionTokenBudget)
	}
}

func TestARepeatedTurnHaltsWithItsOwnError(t *testing.T) {
	same := aiTurn{tcs: []ai.ToolCall{{Name: "read", Arguments: map[string]interface{}{"id": "a"}}}}
	prov := &fakeAI{turns: []aiTurn{same, same, same, same, same, same}}
	drv := &fakeDriver{dispatchFn: func(ai.ToolCall) StepResult {
		return StepResult{Result: "ok", Signature: "read:a"}
	}}
	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", IterateUntilDone: true, NoProgressStop: 100, GraceInstruction: graceInstruction})
	if out.Halt != ErrRepeatedTurn || out.Summary != reasonRepeatedTurn {
		t.Fatalf("outcome %+v, want the repeated-turn halt", out)
	}
	if len(prov.inputs) != 3 {
		t.Fatalf("%d model calls, want the stop on the third identical turn and no grace call", len(prov.inputs))
	}
}
