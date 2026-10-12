package agentloop

import (
	"reflect"
	"testing"

	"vozko/domain/ai"
)

func TestTheNextStepSeesTheReasoningOfTheLastOne(t *testing.T) {
	plan := []ai.ReasoningBlock{{Index: 0, Kind: ai.ReasoningText, Text: "plano: céu, mar, título", Signature: "sig-1", Format: "f1"}}
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("read")}, thinking: plan},
		{fullText: "pronto"},
	}}
	run(t, prov, reading(), Config{FinishToolName: "finish", IterateUntilDone: true})
	var carried []ai.ReasoningBlock
	for _, m := range prov.inputs[1].Messages {
		if m.Role == ai.RoleAssistant && len(m.ToolCalls) > 0 {
			carried = m.Reasoning
		}
	}
	if !reflect.DeepEqual(carried, plan) {
		t.Fatalf("the second call carried %+v, want the first step's reasoning with its signature", carried)
	}
}
