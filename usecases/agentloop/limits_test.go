package agentloop

import (
	"strings"
	"testing"

	"vozko/domain/ai"
)

const graceInstruction = "Responda agora com o que já descobriu."

func reading() *fakeDriver {
	return &fakeDriver{dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Result: strings.Repeat("dado ", 80)} }}
}

func toolTurn(name string, promptTokens int) aiTurn {
	return aiTurn{tcs: []ai.ToolCall{tcall(name)}, usage: &ai.Usage{PromptTokens: promptTokens, TotalTokens: promptTokens}}
}

func TestTheIterationCapEndsWithAGraceAnswerInsteadOfNothing(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{toolTurn("a", 10), toolTurn("b", 10), {fullText: "A Liliam está sem conversas porque está fora do rodízio."}}}
	out, _, sess := run(t, prov, reading(), Config{FinishToolName: "finish", MaxIterations: 2, NoProgressStop: 20, GraceInstruction: graceInstruction})
	if out.Kind != OutcomeIdle || out.Halt != nil {
		t.Fatalf("outcome %+v", out)
	}
	last := sess.History[len(sess.History)-1]
	if last.Role != ai.RoleAssistant || !strings.Contains(last.Content, "fora do rodízio") {
		t.Fatalf("the grace answer must be the reply, got %+v", last)
	}
	grace := prov.inputs[len(prov.inputs)-1]
	if len(grace.Tools) != 0 || !strings.Contains(grace.Messages[len(grace.Messages)-1].Content, graceInstruction) {
		t.Fatalf("the grace call must ask for the answer and offer no tools: %+v", grace)
	}
}

func TestTheCostCeilingEndsWithAGraceAnswer(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{toolTurn("a", 10), toolTurn("b", 10), {fullText: "Parcial: faltou ver a fila."}}}
	cfg := Config{
		FinishToolName: "finish", MaxIterations: 50, NoProgressStop: 20, GraceInstruction: graceInstruction,
		CostCeilingMicros: 150, ModelLimits: ai.ModelInfo{PromptPrice: 10},
	}
	out, _, sess := run(t, prov, reading(), cfg)
	if out.Kind != OutcomeIdle || len(prov.inputs) != 3 || sess.CostMicros != 200 {
		t.Fatalf("outcome %+v calls %d cost %d", out, len(prov.inputs), sess.CostMicros)
	}
	if len(prov.inputs[2].Tools) != 0 {
		t.Fatal("the call after the ceiling must be the grace answer")
	}
}

func TestAnEmptyGraceAnswerKeepsTheOriginalStop(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{toolTurn("a", 10), {fullText: ""}}}
	cfg := Config{FinishToolName: "finish", SessionTokenBudget: 5, GraceInstruction: graceInstruction}
	out, _, _ := run(t, prov, reading(), cfg)
	if out.Kind != OutcomeDone || out.Halt != ErrSessionBudget {
		t.Fatalf("outcome %+v", out)
	}
}

func TestWithoutAGraceInstructionTheLimitsStopAsBefore(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{toolTurn("a", 10), toolTurn("b", 10)}}
	out, _, _ := run(t, prov, reading(), Config{FinishToolName: "finish", CostCeilingMicros: 1, ModelLimits: ai.ModelInfo{PromptPrice: 1}})
	if out.Kind != OutcomeDone || out.Halt != ErrSessionBudget || len(prov.inputs) != 1 {
		t.Fatalf("outcome %+v calls %d", out, len(prov.inputs))
	}
}

func TestOldToolResultsAreClearedWhenTheContextFills(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{toolTurn("a", 10), toolTurn("b", 10), toolTurn("c", 60), {fullText: "pronto"}}}
	cfg := Config{FinishToolName: "finish", NoProgressStop: 20, ModelLimits: ai.ModelInfo{ContextLength: 100}, KeepRecent: 2}
	run(t, prov, reading(), cfg)

	before := prov.inputs[2].Messages
	after := prov.inputs[3].Messages
	if countCleared(before) != 0 {
		t.Fatal("nothing is cleared while the context has room")
	}
	if countCleared(after) != 2 {
		t.Fatalf("the two oldest tool results should be cleared, got %d", countCleared(after))
	}
	if !strings.Contains(after[0].Content, "faça X") {
		t.Fatal("the user request must stay intact")
	}
	recent := after[len(after)-2]
	if recent.Role != ai.RoleTool || strings.Contains(recent.Content, clearedToolResult) {
		t.Fatalf("the newest tool result must stay intact: %+v", recent)
	}
}

func countCleared(msgs []ai.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == ai.RoleTool && m.Content == clearedToolResult {
			n++
		}
	}
	return n
}
