package aichat

import (
	"encoding/json"
	"fmt"

	"vozko/domain/ai"
)

type transcriptMessage struct {
	Role       ai.Role              `json:"role"`
	Content    string               `json:"content,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []transcriptCall     `json:"tool_calls,omitempty"`
	Reasoning  []transcriptThinking `json:"reasoning,omitempty"`
}

type transcriptCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type transcriptThinking struct {
	ID        string           `json:"id,omitempty"`
	Index     int              `json:"index"`
	Kind      ai.ReasoningKind `json:"kind"`
	Text      string           `json:"text,omitempty"`
	Summary   string           `json:"summary,omitempty"`
	Data      string           `json:"data,omitempty"`
	Format    string           `json:"format,omitempty"`
	Signature string           `json:"signature,omitempty"`
}

func EncodeTranscript(turn []ai.Message) ([]byte, error) {
	out := make([]transcriptMessage, 0, len(turn))
	for _, m := range turn {
		if m.Role != ai.RoleAssistant && m.Role != ai.RoleTool {
			continue
		}
		stored := transcriptMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, call := range m.ToolCalls {
			stored.ToolCalls = append(stored.ToolCalls, transcriptCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		for _, b := range m.Reasoning {
			stored.Reasoning = append(stored.Reasoning, transcriptThinking(b))
		}
		out = append(out, stored)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return json.Marshal(out)
}

func DecodeTranscript(raw []byte) ([]ai.Message, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var stored []transcriptMessage
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("aichat: unreadable transcript: %w", err)
	}
	out := make([]ai.Message, 0, len(stored))
	for _, m := range stored {
		msg := ai.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, call := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, ai.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		for _, b := range m.Reasoning {
			msg.Reasoning = append(msg.Reasoning, ai.ReasoningBlock(b))
		}
		out = append(out, msg)
	}
	return out, nil
}

func ReplayFor(turn []ai.Message, producedBy, model string) []ai.Message {
	if producedBy == model {
		return turn
	}
	out := make([]ai.Message, len(turn))
	for i, m := range turn {
		m.Reasoning = nil
		out[i] = m
	}
	return out
}
