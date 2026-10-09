package agentloop

import (
	"context"
	"testing"
	"time"

	"vozko/domain/ai"
)

func TestEveryCallOfARunSharesTheCacheablePrefix(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("look")}},
		{fullText: "pronto"},
	}}
	cfg := Config{WorkspaceID: "ws-1", BillingReference: "aichat:th-1", SessionID: "aichat:th-1", MaxIterations: 1, GraceInstruction: "responda"}
	e := Engine{AI: prov}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, cfg, &Session{}, "faça X")
	if len(prov.inputs) < 2 {
		t.Fatalf("expected the turn and the grace answer, got %d generations", len(prov.inputs))
	}
	first := prov.inputs[0]
	if first.AsOf.IsZero() || time.Since(first.AsOf) > time.Minute {
		t.Fatalf("the run must read the clock once when it starts, got %v", first.AsOf)
	}
	for i, in := range prov.inputs {
		if !in.AsOf.Equal(first.AsOf) || in.SessionID != "aichat:th-1" || in.VolatileTail != 1 {
			t.Fatalf("generation %d must share the clock, session and volatile tail, got %v %q %d", i, in.AsOf, in.SessionID, in.VolatileTail)
		}
		if len(in.Tools) != len(first.Tools) || in.ToolExecutionMode != ai.ToolExecutionModeNone {
			t.Fatalf("generation %d must send the same tools so the cache holds", i)
		}
	}
	if grace := prov.inputs[len(prov.inputs)-1]; grace.ToolChoice != "none" {
		t.Fatalf("the grace answer keeps the tools but must not call them, got %q", grace.ToolChoice)
	}
}

func TestARunKeepsAClockGivenByTheCaller(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{fullText: "pronto"}}}
	asOf := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	e := Engine{AI: prov}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, Config{AsOf: asOf}, &Session{}, "oi")
	if len(prov.inputs) == 0 || !prov.inputs[0].AsOf.Equal(asOf) {
		t.Fatal("a clock set by the caller is used as is")
	}
}
