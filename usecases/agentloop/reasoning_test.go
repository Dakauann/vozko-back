package agentloop

import (
	"context"
	"testing"

	"vozko/domain/ai"
)

func TestTheChosenReasoningLevelReachesEveryModelCall(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{fullText: "pronto"}}}
	e := Engine{AI: prov}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, Config{ReasoningEffort: ai.ReasoningHigh}, &Session{}, "faça X")
	if len(prov.inputs) == 0 || prov.inputs[0].ReasoningEffort != ai.ReasoningHigh {
		t.Fatalf("inputs = %+v, want the high level on the call", prov.inputs)
	}
}

func TestNoReasoningLevelLeavesTheCallOnTheModelDefault(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{fullText: "pronto"}}}
	e := Engine{AI: prov}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, Config{}, &Session{}, "faça X")
	if prov.inputs[0].ReasoningEffort != "" {
		t.Fatalf("effort = %q, want empty", prov.inputs[0].ReasoningEffort)
	}
}
