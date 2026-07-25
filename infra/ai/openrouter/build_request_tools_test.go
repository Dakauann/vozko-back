package openrouter

import (
	"context"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

// injectFakeToolService returns a non-empty default registry so we can assert
// whether buildRequest falls back to it.
type injectFakeToolService struct{}

func (injectFakeToolService) Definitions() []tools.Definition {
	return []tools.Definition{{Name: "manage_entry_stage", Description: "stub"}}
}
func (injectFakeToolService) DefinitionsFor(tools.ToolVisibility) []tools.Definition {
	return []tools.Definition{{Name: "manage_entry_stage", Description: "stub"}}
}
func (injectFakeToolService) Execute(context.Context, string, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (injectFakeToolService) ExecuteWithConfig(context.Context, string, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (injectFakeToolService) Handler(string) (tools.Handler, bool) { return nil, false }

// Regression: a caller that disables tool execution (ToolExecutionModeNone) and
// passes no tools — e.g. a workflow AI-agent node in prompt mode — must NOT have
// the full default registry injected. Injecting it made the model emit tool calls
// it could never run, suppressing its text reply (the "keeps typing, won't
// respond" bug). A normal Auto-mode call with no tools still gets the defaults.
func TestBuildRequest_NoDefaultToolsWhenExecutionDisabled(t *testing.T) {
	s := NewService(Config{DefaultModel: "test/model"}, injectFakeToolService{}, nil)

	reqNone := s.buildRequest(ai.GenerateInput{
		Model:             "test/model",
		Tools:             nil,
		ToolExecutionMode: ai.ToolExecutionModeNone,
	})
	if len(reqNone.Tools) != 0 {
		t.Fatalf("ToolExecutionModeNone + no tools: expected 0 injected tools, got %d", len(reqNone.Tools))
	}

	reqAuto := s.buildRequest(ai.GenerateInput{
		Model: "test/model",
		Tools: nil,
	})
	if len(reqAuto.Tools) == 0 {
		t.Fatalf("execution-allowed + no tools: expected default tools to be injected, got 0")
	}
}

func TestBuildRequest_ResponseFormatJSONObject(t *testing.T) {
	s := NewService(Config{DefaultModel: "test/model"}, nil, nil)
	req := s.buildRequest(ai.GenerateInput{
		Model: "openai/gpt-4o-mini",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "return json"},
		},
		ResponseFormat: &ai.ResponseFormat{Type: ai.ResponseFormatJSONObject},
		ToolExecutionMode: ai.ToolExecutionModeNone,
	})
	if req.ResponseFormat == nil || string(req.ResponseFormat.Type) != "json_object" {
		t.Fatalf("ResponseFormat=%+v", req.ResponseFormat)
	}
}

func TestBuildRequest_ResponseFormatJSONSchema(t *testing.T) {
	s := NewService(Config{DefaultModel: "test/model"}, nil, nil)
	req := s.buildRequest(ai.GenerateInput{
		Model: "openai/gpt-4o-mini",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "return json"},
		},
		ResponseFormat: &ai.ResponseFormat{
			Type:           ai.ResponseFormatJSONSchema,
			JSONSchemaName: "floor",
			JSONSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"decision": map[string]any{"type": "string"},
				},
				"required": []any{"decision"},
			},
			JSONSchemaStrict: true,
		},
		ToolExecutionMode: ai.ToolExecutionModeNone,
	})
	if req.ResponseFormat == nil || string(req.ResponseFormat.Type) != "json_schema" {
		t.Fatalf("ResponseFormat=%+v", req.ResponseFormat)
	}
	if req.ResponseFormat.JSONSchema == nil || req.ResponseFormat.JSONSchema.Name != "floor" {
		t.Fatalf("JSONSchema=%+v", req.ResponseFormat.JSONSchema)
	}
	if !req.ResponseFormat.JSONSchema.Strict {
		t.Fatal("expected strict")
	}
}
