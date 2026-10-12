package openrouter

import (
	"reflect"
	"testing"

	sdk "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
)

func TestStreamedReasoningPiecesMergeByIndex(t *testing.T) {
	var acc reasoningAccumulator
	acc.add([]sdk.ChatCompletionReasoningDetails{{Index: 0, Type: sdk.ReasoningDetailsTypeText, Text: "plano: ", Format: "f1"}})
	acc.add([]sdk.ChatCompletionReasoningDetails{{Index: 0, Type: sdk.ReasoningDetailsTypeText, Text: "céu e mar"}, {Index: 1, Type: sdk.ReasoningDetailsTypeEncrypted, Data: "abc"}})
	acc.add([]sdk.ChatCompletionReasoningDetails{{Index: 0, Type: sdk.ReasoningDetailsTypeText, Signature: "sig-1"}, {Index: 1, Type: sdk.ReasoningDetailsTypeEncrypted, Data: "def"}})
	want := []ai.ReasoningBlock{
		{Index: 0, Kind: ai.ReasoningText, Text: "plano: céu e mar", Format: "f1", Signature: "sig-1"},
		{Index: 1, Kind: ai.ReasoningEncrypted, Data: "abcdef"},
	}
	if got := acc.blocks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks = %+v, want %+v", got, want)
	}
}

func TestAnAssistantStepSendsItsReasoningBack(t *testing.T) {
	s := &Service{defaultModel: "anthropic/claude-sonnet-4"}
	thinking := []ai.ReasoningBlock{{Index: 0, Kind: ai.ReasoningText, Text: "plano", Signature: "sig-1", Format: "f1"}}
	req := s.buildRequest(ai.GenerateInput{Messages: []ai.Message{
		{Role: ai.RoleUser, Content: "faça"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read"}}, Reasoning: thinking},
		{Role: ai.RoleTool, ToolCallID: "c1", Content: "ok"},
	}})
	got := req.Messages[1].ReasoningDetails
	want := []sdk.ChatCompletionReasoningDetails{{Index: 0, Type: sdk.ReasoningDetailsTypeText, Text: "plano", Signature: "sig-1", Format: "f1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reasoning_details = %+v, want %+v", got, want)
	}
}
