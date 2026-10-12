package agentloop

import (
	"testing"

	"vozko/domain/ai"
)

func TestTheSessionKeepsTheRepliesOwnMessages(t *testing.T) {
	plan := []ai.ReasoningBlock{{Kind: ai.ReasoningText, Text: "plano"}}
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("read")}, thinking: plan},
		{fullText: "pronto"},
	}}
	_, _, sess := run(t, prov, reading(), Config{FinishToolName: "finish", IterateUntilDone: true})
	if len(sess.Turn) != 3 {
		t.Fatalf("turn = %+v, want the tool call, its result and the answer", sess.Turn)
	}
	if sess.Turn[0].Role != ai.RoleAssistant || len(sess.Turn[0].Reasoning) != 1 || sess.Turn[1].Role != ai.RoleTool || sess.Turn[2].Content != "pronto" {
		t.Fatalf("turn = %+v", sess.Turn)
	}
	for _, m := range sess.Turn {
		if m.Role == ai.RoleUser {
			t.Fatalf("the request and notes are not part of the reply: %+v", m)
		}
	}
}
