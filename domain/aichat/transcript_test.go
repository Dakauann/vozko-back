package aichat

import (
	"reflect"
	"testing"

	"vozko/domain/ai"
)

func TestATranscriptSurvivesStorage(t *testing.T) {
	turn := []ai.Message{
		{Role: ai.RoleAssistant, Content: "vou ler", ToolCalls: []ai.ToolCall{{ID: "c1", Name: "studio_read", Arguments: map[string]interface{}{"artboard_id": "ab-1"}}},
			Reasoning: []ai.ReasoningBlock{{Index: 0, Kind: ai.ReasoningText, Text: "plano", Signature: "sig", Format: "f1"}}},
		{Role: ai.RoleTool, ToolCallID: "c1", Content: `{"layers":[]}`},
		{Role: ai.RoleAssistant, Content: "pronto"},
	}
	raw, err := EncodeTranscript(turn)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeTranscript(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, turn) {
		t.Fatalf("decoded %+v, want %+v", back, turn)
	}
}

func TestATranscriptKeepsOnlyTheAgentsOwnMessagesAndNoImages(t *testing.T) {
	raw, err := EncodeTranscript([]ai.Message{
		{Role: ai.RoleUser, Content: "nudge"},
		{Role: ai.RoleAssistant, Content: "ok", Images: []string{"data:image/png;base64,AAAA"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	back, _ := DecodeTranscript(raw)
	if len(back) != 1 || back[0].Role != ai.RoleAssistant || len(back[0].Images) != 0 {
		t.Fatalf("decoded %+v, want only the assistant message without images", back)
	}
}

func TestNoTranscriptDecodesToNothing(t *testing.T) {
	back, err := DecodeTranscript(nil)
	if err != nil || back != nil {
		t.Fatalf("decoded %+v, %v", back, err)
	}
	if _, err := DecodeTranscript([]byte("{broken")); err == nil {
		t.Fatal("a broken transcript must be refused, never half read")
	}
}

func TestReasoningIsReplayedOnlyToTheModelThatProducedIt(t *testing.T) {
	turn := []ai.Message{{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "x"}}, Reasoning: []ai.ReasoningBlock{{Kind: ai.ReasoningEncrypted, Data: "opaque"}}}}
	if got := ReplayFor(turn, "m1", "m1"); len(got[0].Reasoning) != 1 {
		t.Fatal("the same model must get its reasoning back")
	}
	other := ReplayFor(turn, "m1", "m2")
	if len(other[0].Reasoning) != 0 || len(turn[0].Reasoning) != 1 {
		t.Fatal("another model must get the transcript without the reasoning, and the original must stay intact")
	}
}
