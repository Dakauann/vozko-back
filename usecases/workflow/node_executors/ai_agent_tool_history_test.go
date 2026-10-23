package node_executors

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/workflow"
)

type toolRoundTripAI struct {
	ai.Service
	inputs    []ai.GenerateInput
	repeat    bool
	afterCall func()
}

func (s *toolRoundTripAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	s.inputs = append(s.inputs, in)
	if s.afterCall != nil {
		s.afterCall()
	}
	for _, message := range in.Messages {
		if message.Role == ai.RoleTool && !s.repeat {
			return &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: "Tenho horários disponíveis. Qual você prefere?"}}, nil
		}
	}
	return &ai.GenerateOutput{
		Message:   ai.Message{Role: ai.RoleAssistant, Reasoning: []ai.ReasoningBlock{{Kind: ai.ReasoningEncrypted, Data: "reasoning-state"}}},
		ToolCalls: []ai.ToolCall{{ID: "call-availability", Name: "verificar_disponibilidade", Arguments: map[string]interface{}{"date_from": "2026-10-23", "date_to": "2026-10-23"}}},
	}, nil
}

func toolAgentContext(state *workflow.RunState) *workflow.NodeContext {
	ctx := agentNodeContext(state, map[string]interface{}{
		"custom_tools": []interface{}{map[string]interface{}{"name": "verificar_disponibilidade", "description": "Consulta os horários disponíveis"}},
	})
	ctx.Graph = &workflow.Graph{
		Nodes: []workflow.Node{*ctx.Node, {ID: "calendar", Type: workflow.NodeTypeActionCheckCalendarAvailability}, {ID: "wait", Type: workflow.NodeTypeWaitForReply}},
		Edges: []workflow.Edge{{Source: ctx.Node.ID, Target: "calendar", Label: "verificar_disponibilidade"}, {Source: "calendar", Target: ctx.Node.ID, Label: "sucesso"}, {Source: ctx.Node.ID, Target: "wait", Label: "default"}},
	}
	return ctx
}

func returnAvailability(ctx *workflow.NodeContext) {
	workflow.RecordNodeOutput(ctx.State, "calendar", map[string]interface{}{"success": true, "available_slots": []interface{}{map[string]interface{}{"start": "2026-10-23T09:00:00-03:00"}}})
	ctx.State.Set("_prev_node_id", "calendar")
}

func TestToolResultReturnsAsAMatchedToolMessageAndTheAgentCanWaitForTheCustomer(t *testing.T) {
	service := &toolRoundTripAI{}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	ctx := toolAgentContext(&state)
	first, err := executor.Execute(ctx)
	if err != nil || first.NextNodeID != "calendar" {
		t.Fatalf("first tool call: %+v %v", first, err)
	}
	returnAvailability(ctx)
	raw, err := state.JSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := workflow.RunStateFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx.State = &restored
	second, err := executor.Execute(ctx)
	if err != nil || second.NextNodeID != "wait" || len(service.inputs) != 2 {
		t.Fatalf("the result must allow a reply instead of repeating the tool: %+v %v", second, err)
	}
	var assistant, tool *ai.Message
	for i := range service.inputs[1].Messages {
		message := &service.inputs[1].Messages[i]
		if len(message.ToolCalls) > 0 {
			assistant = message
		}
		if message.Role == ai.RoleTool {
			tool = message
		}
		if message.Role == ai.RoleUser && strings.Contains(message.Content, "available_slots") {
			t.Fatal("tool output was disguised as a user message")
		}
	}
	if assistant == nil || tool == nil || assistant.ToolCalls[0].ID != tool.ToolCallID || len(assistant.Reasoning) != 1 {
		t.Fatalf("the assistant call, reasoning and matching result were not preserved: %+v", service.inputs[1].Messages)
	}
}

func TestRepeatedIdenticalToolsStopBeforeAnotherCalendarRequest(t *testing.T) {
	service := &toolRoundTripAI{repeat: true}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	ctx := toolAgentContext(&state)
	for i := 0; i < maxIdenticalWorkflowToolCalls; i++ {
		result, err := executor.Execute(ctx)
		if err != nil || result.NextNodeID != "calendar" {
			t.Fatalf("tool call %d: %+v %v", i, result, err)
		}
		returnAvailability(ctx)
	}
	result, err := executor.Execute(ctx)
	if err != nil || result.ErrorKind != workflow.ErrorKindGuard || result.NextNodeID != "" || !strings.Contains(result.Error, "tool loop detected") {
		t.Fatalf("the repeated tool must stop visibly: %+v %v", result, err)
	}
	ctx.State.Set("_prev_node_id", "wait")
	result, err = executor.Execute(ctx)
	if err != nil || result.NextNodeID != "calendar" {
		t.Fatalf("a new customer turn must allow the same dates again: %+v %v", result, err)
	}
}

func TestACancelDuringGenerationDiscardsTheAnswerAndTheNextToolCall(t *testing.T) {
	active := true
	service := &toolRoundTripAI{afterCall: func() { active = false }}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	ctx := toolAgentContext(&state)
	ctx.EnsureActive = func() error {
		if !active {
			return workflow.ErrRunTerminal
		}
		return nil
	}
	result, err := executor.Execute(ctx)
	if result != nil || !errors.Is(err, workflow.ErrRunTerminal) {
		t.Fatalf("cancelled generation must stop: %+v %v", result, err)
	}
	if _, exists := state.Get("_pending_tool_call"); exists {
		t.Fatal("cancelled output started another tool call")
	}
}

func TestUnreadableToolHistoryBlocksTheModelCall(t *testing.T) {
	service := &toolRoundTripAI{}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	ctx := toolAgentContext(&state)
	state.Set(toolHistoryKey(ctx.Node.ID), "invalid json")
	result, err := executor.Execute(ctx)
	if err != nil || result.ErrorKind != workflow.ErrorKindConfig || len(service.inputs) != 0 {
		t.Fatalf("invalid history must fail closed: %+v %v", result, err)
	}
}
