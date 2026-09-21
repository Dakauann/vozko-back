package agentloop

import (
	"strings"
	"testing"

	"vozko/domain/ai"
)

func TestRepeatedTurnsEndTheLoopEvenWhenStateKeepsChanging(t *testing.T) {
	sameCall := aiTurn{tcs: []ai.ToolCall{{
		Name:      "update_node",
		Arguments: map[string]interface{}{"node_id": "n11"},
	}}}
	prov := &fakeAI{turns: []aiTurn{sameCall, sameCall, sameCall, sameCall, sameCall, sameCall}}

	edits := 0
	drv := &fakeDriver{
		dispatchFn: func(call ai.ToolCall) StepResult {
			edits++
			return StepResult{Result: "ok", Mutated: true, Signature: "update_node:n11"}
		},
		progressFn: func() Progress {
			return Progress{
				StateHash:         strings.Repeat("x", edits),
				BlockingSignature: "",
				Valid:             true,
			}
		},
	}

	out, _, _ := run(t, prov, drv, Config{
		FinishToolName:   "finish",
		MaxIterations:    30,
		RepeatedTurnStop: 3,
	})

	if out.Summary != reasonRepeatedTurn {
		t.Errorf("summary = %q, want the repeated-turn stall reason", out.Summary)
	}
	if edits > 4 {
		t.Errorf("made %d edits, want the guard to fire at 3", edits)
	}
	if !out.Valid {
		t.Error("a valid graph must stay valid when the loop stalls")
	}
}

func TestDifferentTargetsAreNotTreatedAsRepetition(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("add_node")}},
		{tcs: []ai.ToolCall{tcall("add_node")}},
		{tcs: []ai.ToolCall{tcall("add_node")}},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}

	n := 0
	drv := &fakeDriver{
		dispatchFn: func(call ai.ToolCall) StepResult {
			n++
			return StepResult{Result: "ok", Mutated: true, Signature: "add_node:n" + string(rune('0'+n))}
		},
	}

	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", MaxIterations: 30, RepeatedTurnStop: 3})
	if out.Summary == reasonRepeatedTurn {
		t.Error("three edits to three different nodes is progress, not churn")
	}
	if !out.Valid {
		t.Errorf("expected the finish to be honored, got %+v", out)
	}
}

func TestNoSignatureDisablesTheGuard(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("noop")}},
		{tcs: []ai.ToolCall{tcall("noop")}},
		{tcs: []ai.ToolCall{tcall("noop")}},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}
	drv := &fakeDriver{
		dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Result: "ok", Mutated: true} },
	}

	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", MaxIterations: 30, RepeatedTurnStop: 3})
	if out.Summary == reasonRepeatedTurn {
		t.Error("an empty signature must not trip the guard")
	}
}

func TestTheRequestIsAnchoredOnceAndNeverRepeated(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("noop")}},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}
	drv := &fakeDriver{}

	_, _, sess := run(t, prov, drv, Config{FinishToolName: "finish", MaxIterations: 5})

	anchors := 0
	for _, m := range sess.History {
		if m.Role == ai.RoleUser && strings.Contains(m.Content, "faça X") {
			anchors++
		}
	}
	if anchors != 1 {
		t.Errorf("the request appears %d times in history, want exactly 1", anchors)
	}
}
