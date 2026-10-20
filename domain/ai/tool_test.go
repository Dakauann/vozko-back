package ai

import "testing"

func TestAToolCallKeepsTheModelsIDAndGetsOneOnlyWhenItHasNone(t *testing.T) {
	calls := []ToolCall{{ID: "call_abc", Name: "a"}, {Name: "b"}, {ID: "  ", Name: "c"}}
	EnsureToolCallIDs(calls)
	if calls[0].ID != "call_abc" {
		t.Fatalf("the model's id was replaced: %q", calls[0].ID)
	}
	if calls[1].ID == "" || calls[2].ID == "" || calls[1].ID == calls[2].ID {
		t.Fatalf("calls without an id need distinct ids: %q, %q", calls[1].ID, calls[2].ID)
	}
	second := calls[1].ID
	EnsureToolCallIDs(calls)
	if calls[1].ID != second {
		t.Fatal("an id once given must never change, or the record and the history would drift apart")
	}
}
