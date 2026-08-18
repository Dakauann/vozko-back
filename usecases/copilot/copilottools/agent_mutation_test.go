package copilottools

import (
	"context"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/copilot"
)

type fakeGetAgent struct{ a *agent.Agent }

func (f fakeGetAgent) Execute(id string) (*agent.Agent, error) {
	if f.a == nil || f.a.ID != id {
		return nil, nil
	}
	return f.a, nil
}

type fakeUpdateAgent struct {
	gotID string
	got   agent.UpdateAgentInput
	calls int
}

func (f *fakeUpdateAgent) Execute(_ context.Context, id string, in agent.UpdateAgentInput) (*agent.Agent, error) {
	f.gotID, f.got, f.calls = id, in, f.calls+1
	return &agent.Agent{ID: id}, nil
}

type fakeCreateAgent struct{ got agent.CreateAgentInput }

func (f *fakeCreateAgent) Execute(_ context.Context, in agent.CreateAgentInput) (*agent.Agent, error) {
	f.got = in
	return &agent.Agent{ID: "new"}, nil
}

func boundAgent() *agent.Agent {
	return &agent.Agent{
		ID:               "ag-1",
		WorkspaceID:      "ws-1",
		InternalTools:    []agent.ToolBinding{{Name: "manage_lead_memory"}, {Name: "search_knowledge_base"}},
		KnowledgeBaseIDs: []string{"kb-1"},
	}
}

func okContext() copilot.Context { return copilot.Context{WorkspaceID: "ws-1"} }

// The bug this whole change exists for: the assistant asked to add a tool, the
// update reported success, and the tool was never there, because update_agent
// had no parameter able to carry it.
func TestUpdateAgentAddsAToolAndKeepsTheExistingOnes(t *testing.T) {
	upd := &fakeUpdateAgent{}
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, upd)

	if _, ok := tool.Definition().Parameters["addTools"]; !ok {
		t.Fatal("update_agent must expose a parameter for tools")
	}

	res := tool.Execute(context.Background(), okContext(), map[string]interface{}{
		"id": "ag-1",
		"addTools": []interface{}{map[string]interface{}{
			"name":   "http_request",
			"config": map[string]interface{}{"url": "https://viacep.com.br/ws/{cep}/json/", "method": "GET"},
		}},
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}

	names := map[string]map[string]interface{}{}
	for _, tb := range upd.got.InternalTools {
		names[tb.Name] = tb.Config
	}
	for _, want := range []string{"manage_lead_memory", "search_knowledge_base", "http_request"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("tool %q missing from the update: %+v", want, upd.got.InternalTools)
		}
	}
	if names["http_request"]["method"] != "GET" {
		t.Fatalf("config not carried through: %+v", names["http_request"])
	}
}

// A model that only names the tool it wants must never be able to wipe the
// rest, which is why the parameter is addTools and not internalTools.
func TestUpdateAgentRemovesOnlyWhatWasAsked(t *testing.T) {
	upd := &fakeUpdateAgent{}
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, upd)

	res := tool.Execute(context.Background(), okContext(), map[string]interface{}{
		"id":          "ag-1",
		"removeTools": []interface{}{"manage_lead_memory"},
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	if len(upd.got.InternalTools) != 1 || upd.got.InternalTools[0].Name != "search_knowledge_base" {
		t.Fatalf("tools = %+v", upd.got.InternalTools)
	}
}

// Touching only the prompt must leave membership alone: nil is how
// ApplyUpdate is told "keep what is there".
func TestUpdateAgentLeavesMembershipUntouchedWhenNotMentioned(t *testing.T) {
	upd := &fakeUpdateAgent{}
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, upd)

	tool.Execute(context.Background(), okContext(), map[string]interface{}{
		"id":              "ag-1",
		"messagingPrompt": "novo prompt",
	})
	if upd.got.InternalTools != nil || upd.got.KnowledgeBaseIDs != nil || upd.got.MCPCollectionIDs != nil {
		t.Fatalf("membership must stay nil when unmentioned: %+v", upd.got)
	}
	if upd.got.MessagingPrompt == nil || *upd.got.MessagingPrompt != "novo prompt" {
		t.Fatalf("scalar field did not bind: %+v", upd.got)
	}
}

// Re-adding a bound tool is how a misconfiguration gets corrected.
func TestUpdateAgentReAddReplacesConfig(t *testing.T) {
	current := boundAgent()
	current.InternalTools = append(current.InternalTools, agent.ToolBinding{
		Name: "http_request", Config: map[string]interface{}{"method": "POST"},
	})
	upd := &fakeUpdateAgent{}
	tool := NewUpdateAgentTool(fakeGetAgent{a: current}, upd)

	tool.Execute(context.Background(), okContext(), map[string]interface{}{
		"id": "ag-1",
		"addTools": []interface{}{map[string]interface{}{
			"name": "http_request", "config": map[string]interface{}{"method": "GET"},
		}},
	})
	count := 0
	for _, tb := range upd.got.InternalTools {
		if tb.Name != "http_request" {
			continue
		}
		count++
		if tb.Config["method"] != "GET" {
			t.Fatalf("config not replaced: %+v", tb.Config)
		}
	}
	if count != 1 {
		t.Fatalf("re-adding must replace, not duplicate: %+v", upd.got.InternalTools)
	}
}

// The workspace gate must fire before anything is read off the agent, so a
// foreign agent is indistinguishable from a missing one.
func TestUpdateAgentDeniesForeignWorkspace(t *testing.T) {
	foreign := boundAgent()
	foreign.WorkspaceID = "ws-OTHER"
	upd := &fakeUpdateAgent{}
	tool := NewUpdateAgentTool(fakeGetAgent{a: foreign}, upd)

	res := tool.Execute(context.Background(), okContext(), map[string]interface{}{
		"id":          "ag-1",
		"addTools":    []interface{}{map[string]interface{}{"name": "http_request"}},
		"workspaceId": "ws-OTHER",
	})
	if res.Status != copilot.StatusDenied {
		t.Fatalf("status = %v, want denied", res.Status)
	}
	if upd.calls != 0 {
		t.Fatal("a denied call must never reach the update use case")
	}
}

func TestUpdateAgentDeniesOutOfScopeDepartment(t *testing.T) {
	a := boundAgent()
	a.DepartmentID = "dept-b"
	upd := &fakeUpdateAgent{}
	tool := NewUpdateAgentTool(fakeGetAgent{a: a}, upd)

	res := tool.Execute(context.Background(), copilot.Context{WorkspaceID: "ws-1", DeptScope: []string{"dept-a"}},
		map[string]interface{}{"id": "ag-1", "removeTools": []interface{}{"search_knowledge_base"}})
	if res.Status != copilot.StatusDenied || upd.calls != 0 {
		t.Fatalf("status = %v calls = %d", res.Status, upd.calls)
	}
}

func TestCreateAgentCarriesToolsAndAttachments(t *testing.T) {
	crt := &fakeCreateAgent{}
	tool := NewCreateAgentTool(crt)

	res := tool.Execute(context.Background(), okContext(), map[string]interface{}{
		"name":             "Bia",
		"messagingPrompt":  "Você é a Bia.",
		"messagingModel":   "test/model",
		"provider":         "platform",
		"internalTools":    []interface{}{map[string]interface{}{"name": "search_knowledge_base"}},
		"knowledgeBaseIds": []interface{}{"kb-1", "kb-2"},
		"mcpCollectionIds": []interface{}{"mcp-1"},
		"workspaceId":      "ws-EVIL",
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	if crt.got.WorkspaceID != "ws-1" {
		t.Fatalf("workspace must come from the authenticated context, got %q", crt.got.WorkspaceID)
	}
	if len(crt.got.InternalTools) != 1 || crt.got.InternalTools[0].Name != "search_knowledge_base" {
		t.Fatalf("tools = %+v", crt.got.InternalTools)
	}
	if len(crt.got.KnowledgeBaseIDs) != 2 || len(crt.got.MCPCollectionIDs) != 1 {
		t.Fatalf("attachments = %+v / %+v", crt.got.KnowledgeBaseIDs, crt.got.MCPCollectionIDs)
	}
	if crt.got.Name != "Bia" || crt.got.Provider != agent.AgentProvider("platform") {
		t.Fatalf("scalars = %+v", crt.got)
	}
}

// An agent with no tools is legitimate, but the create use case rejects a NIL
// selection, so the empty case must stay non-nil.
func TestCreateAgentWithoutToolsSendsEmptyNotNil(t *testing.T) {
	crt := &fakeCreateAgent{}
	NewCreateAgentTool(crt).Execute(context.Background(), okContext(), map[string]interface{}{
		"name": "Bia", "messagingPrompt": "p", "messagingModel": "m", "provider": "platform",
	})
	if crt.got.InternalTools == nil {
		t.Fatal("internal tools must be empty, not nil")
	}
}

// The array-of-objects schema has to reach the provider with an item shape;
// a bare {"type":"array"} leaves the model guessing the element fields.
func TestToolListParamDeclaresItsItemShape(t *testing.T) {
	p := NewUpdateAgentTool(nil, nil).Definition().Parameters["addTools"]
	if p.Type != "array" || p.Items == nil || p.Items.Type != "object" {
		t.Fatalf("addTools schema = %+v", p)
	}
	if _, ok := p.Items.Properties["name"]; !ok {
		t.Fatalf("item properties = %+v", p.Items.Properties)
	}
	if _, ok := p.Items.Properties["config"]; !ok {
		t.Fatalf("item properties = %+v", p.Items.Properties)
	}
}
