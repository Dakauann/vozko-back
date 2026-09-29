package tools_usecase

import (
	"context"
	"testing"

	"vozko/domain/actor"
)

func TestAStageMoveByAnAgentIsTheAgents(t *testing.T) {
	got := (&manageEntryStageTool{}).moveActor(context.Background(), map[string]interface{}{"__agent_id": "agent-1"})
	if got != actor.FormatAI("agent-1") {
		t.Fatalf("actor = %q", got)
	}
}

func TestAStageMoveWithoutAnAgentIsThePlatformAI(t *testing.T) {
	got := (&manageEntryStageTool{}).moveActor(context.Background(), map[string]interface{}{})
	if got != actor.PlatformAI {
		t.Fatalf("the 5-minute analysis moves stages as the platform AI, got %q", got)
	}
}
