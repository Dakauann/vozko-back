package agentloop

import (
	"context"
	"testing"

	"vozko/domain/ai"
)

func TestEveryGenerationCarriesTheSessionBillingReference(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("look")}},
		{fullText: "pronto"},
	}}
	cfg := Config{WorkspaceID: "ws-1", BillingReference: "aichat:th-1", MaxIterations: 1, GraceInstruction: "responda"}
	e := Engine{AI: prov}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, cfg, &Session{}, "faça X")
	if len(prov.inputs) < 2 {
		t.Fatalf("expected the turn and the grace answer, got %d generations", len(prov.inputs))
	}
	for i, in := range prov.inputs {
		if in.BillingReference != "aichat:th-1" || in.WorkspaceID != "ws-1" {
			t.Fatalf("generation %d must bill the session reference, got %q in %q", i, in.BillingReference, in.WorkspaceID)
		}
	}
}
