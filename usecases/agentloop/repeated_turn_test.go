package agentloop

import (
	"strings"
	"testing"

	"vozko/domain/ai"
)

// The failure this guards against, observed in the workflow builder: a session
// whose graph was already VALID spent its whole iteration budget re-editing one
// node, chasing advisory hints it could not satisfy.
//
// Neither existing guard caught it:
//   - Guard A compares the state hash, and each micro-edit changed it.
//   - Guard B compares the BLOCKING signature, which is empty when nothing is
//     blocking, and an empty signature skips the guard entirely.
//
// Guard C compares what the model ACTED ON, so slightly different arguments to
// the same target still read as standing still.

func TestRepeatedTurnsEndTheLoopEvenWhenStateKeepsChanging(t *testing.T) {
	// The same call, every turn — fakeAI repeats the zero turn once its script
	// runs out, so this keeps answering update_node forever.
	sameCall := aiTurn{tcs: []ai.ToolCall{{
		Name:      "update_node",
		Arguments: map[string]interface{}{"node_id": "n11"},
	}}}
	prov := &fakeAI{turns: []aiTurn{sameCall, sameCall, sameCall, sameCall, sameCall, sameCall}}

	edits := 0
	drv := &fakeDriver{
		dispatchFn: func(call ai.ToolCall) StepResult {
			edits++
			// Mutated, and the state hash differs every turn — exactly the shape
			// that defeats guard A.
			return StepResult{Result: "ok", Mutated: true, Signature: "update_node:n11"}
		},
		progressFn: func() Progress {
			return Progress{
				StateHash: strings.Repeat("x", edits),
				// Nothing blocking: the graph is valid, which is what made the
				// builder's churn guard inert.
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
	// It must stop promptly, not burn the whole 30-iteration budget.
	if edits > 4 {
		t.Errorf("made %d edits, want the guard to fire at 3", edits)
	}
	// A valid graph that merely stopped converging is a finished workflow, not a
	// failure — the user keeps their work.
	if !out.Valid {
		t.Error("a valid graph must stay valid when the loop stalls")
	}
}

// Real progress touches different targets, and must not be mistaken for churn.
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

// A Driver that reports no signature keeps the old behaviour exactly.
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

// The per-turn observation is appended as the newest message every iteration.
// If it restated the request, the model would read it as a question just asked
// and answer it again on every turn — which is exactly what went wrong. The
// Driver interface no longer receives the prompt, so this asserts the engine
// still anchors it exactly once.
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
