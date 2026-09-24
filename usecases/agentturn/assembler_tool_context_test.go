package agentturn

import (
	"context"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

type contextRecordingTool struct{ seen *tools.ToolContext }

func (t contextRecordingTool) Definition() tools.Definition {
	return tools.Definition{Name: "manage_entry_stage"}
}
func (t contextRecordingTool) Execute(context.Context, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (t contextRecordingTool) ExecuteWithConfig(context.Context, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (t contextRecordingTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	*t.seen = ctx
	return t.Definition()
}

type conversationToolRegistry struct {
	stubRegistry
	tool contextRecordingTool
}

func (r conversationToolRegistry) Handler(string) (tools.Handler, bool) { return r.tool, true }

// A tool that tailors itself to the conversation (the stage tool lists only
// the conversation's own pipeline) must be told which conversation it is for.
func TestContextualToolsSeeTheConversation(t *testing.T) {
	seen := &tools.ToolContext{}
	reg := conversationToolRegistry{
		stubRegistry: stubRegistry{defs: []tools.Definition{{Name: "manage_entry_stage"}}},
		tool:         contextRecordingTool{seen: seen},
	}

	New(reg, nil, nil).Assemble(context.Background(), Request{
		Agent:                &agent.Agent{WorkspaceID: "ws1", InternalTools: []agent.ToolBinding{{Name: "manage_entry_stage"}}},
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		EntryID:              "conv-1",
		EntryType:            "unofficial_whatsapp",
	})

	if seen.EntryID != "conv-1" || seen.EntryType != "unofficial_whatsapp" || seen.WorkspaceID != "ws1" {
		t.Fatalf("tool context = %+v, want the conversation conv-1 (unofficial_whatsapp) in ws1", *seen)
	}
}
