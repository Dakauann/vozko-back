package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

type captureCtxToolHandler struct {
	def      tools.Definition
	captured tools.ToolContext
	calls    int
}

func (h *captureCtxToolHandler) Definition() tools.Definition { return h.def }
func (h *captureCtxToolHandler) Execute(context.Context, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (h *captureCtxToolHandler) ExecuteWithConfig(context.Context, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (h *captureCtxToolHandler) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	h.calls++
	h.captured = ctx
	return h.def
}

type fakeToolRegistry struct {
	name    string
	handler *captureCtxToolHandler
}

func (r *fakeToolRegistry) Definitions() []tools.Definition {
	return []tools.Definition{r.handler.def}
}
func (r *fakeToolRegistry) DefinitionsFor(tools.ToolVisibility) []tools.Definition {
	return r.Definitions()
}
func (r *fakeToolRegistry) Execute(context.Context, string, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (r *fakeToolRegistry) ExecuteWithConfig(context.Context, string, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (r *fakeToolRegistry) Handler(name string) (tools.Handler, bool) {
	if name == r.name {
		return r.handler, true
	}
	return nil, false
}

func TestResolveAgentTools_PassesAgentOnToolContext(t *testing.T) {
	handler := &captureCtxToolHandler{
		def: tools.Definition{
			Name:        "send_whatsapp_image",
			Description: "stub",
			Parameters:  map[string]tools.Parameter{},
		},
	}
	registry := &fakeToolRegistry{name: "send_whatsapp_image", handler: handler}

	uc := &handleWhatsAppMessageUseCase{toolRegistry: registry}

	ag := &agent.Agent{
		ID:          "agent-1",
		WorkspaceID: "ws-1",
		MediaIDs:    []string{"media-1", "media-2"},
		InternalTools: []agent.ToolBinding{
			{Name: "send_whatsapp_image"},
		},
	}

	got := uc.resolveAgentTools(ag, "campaign-1", "whatsapp")

	if len(got) != 1 {
		t.Fatalf("expected 1 resolved tool, got %d", len(got))
	}
	if handler.calls != 1 {
		t.Fatalf("expected DefinitionWithContext to be called exactly once, got %d", handler.calls)
	}

	if handler.captured.WorkspaceID != "ws-1" {
		t.Errorf("expected WorkspaceID ws-1 on ToolContext, got %q", handler.captured.WorkspaceID)
	}
	if handler.captured.CampaignID != "campaign-1" {
		t.Errorf("expected CampaignID campaign-1 on ToolContext, got %q", handler.captured.CampaignID)
	}
	if handler.captured.CampaignType != "whatsapp" {
		t.Errorf("expected CampaignType whatsapp on ToolContext, got %q", handler.captured.CampaignType)
	}

	gotAgent, ok := handler.captured.Agent.(*agent.Agent)
	if !ok || gotAgent == nil {
		t.Fatalf("expected ToolContext.Agent to be the *agent.Agent passed to resolveAgentTools, got %#v", handler.captured.Agent)
	}
	if gotAgent.ID != "agent-1" || len(gotAgent.MediaIDs) != 2 {
		t.Fatalf("expected the same agent with its MediaIDs forwarded, got id=%q mediaIDs=%v", gotAgent.ID, gotAgent.MediaIDs)
	}
}
