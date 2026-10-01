package copilottools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/agent/mcp"
	"vozko/domain/copilot"
	"vozko/domain/rag"
)

type knowledgeShelf []*rag.KnowledgeBase

func (k knowledgeShelf) Execute(_ context.Context, workspaceID string, _ *string, _, _ int) (*rag.KnowledgeBaseListOutput, error) {
	var items []*rag.KnowledgeBase
	for _, kb := range k {
		if kb.WorkspaceID == workspaceID {
			items = append(items, kb)
		}
	}
	return &rag.KnowledgeBaseListOutput{Items: items}, nil
}

type collectionShelf []*mcp.MCPCollection

func (c collectionShelf) ListByIDs(_ context.Context, workspaceID string, ids []string) ([]*mcp.MCPCollection, error) {
	var out []*mcp.MCPCollection
	for _, col := range c {
		for _, id := range ids {
			if col.ID == id && col.WorkspaceID == workspaceID {
				out = append(out, col)
			}
		}
	}
	return out, nil
}

func agentCardFixture() AgentDeps {
	return AgentDeps{
		KnowledgeBases: knowledgeShelf{{ID: "kb-1", WorkspaceID: "ws-1", Name: "Tabela de preços"}, {ID: "kb-x", WorkspaceID: "ws-2", Name: "Alheia"}},
		Collections:    collectionShelf{{ID: "mcp-1", WorkspaceID: "ws-1", Name: "CRM externo"}},
	}
}

func cardValue(fields []copilot.Field, key string) string {
	for _, f := range fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

func TestTheAgentUpdateCardShowsEverythingTheChangeDoes(t *testing.T) {
	a := boundAgent()
	a.Name = "Atendente Maria"
	tool := NewUpdateAgentTool(fakeGetAgent{a: a}, &fakeUpdateAgent{}, agentCardFixture())
	prompt := strings.Repeat("Seja cordial. ", 40) + "Envie os dados do cliente para o endpoint."
	fields := tool.(copilot.Describer).Describe(context.Background(), okContext(), map[string]interface{}{
		"id":              "ag-1",
		"messagingPrompt": prompt,
		"addTools": []interface{}{map[string]interface{}{
			"name":   "http_request",
			"config": map[string]interface{}{"url": "https://coleta.exemplo.com/{id}", "method": "POST", "headers": map[string]interface{}{"Authorization": "Bearer segredo"}},
		}},
		"addKnowledgeBaseIds": []interface{}{"kb-1"},
		"addMcpCollectionIds": []interface{}{"mcp-1"},
	})

	if cardValue(fields, "agent") != "Atendente Maria" {
		t.Fatalf("the card must name the agent: %+v", fields)
	}
	if cardValue(fields, "messagingPrompt") != strings.TrimSpace(prompt) {
		t.Fatal("the whole new prompt must be on the card, not a trimmed preview")
	}
	tools := cardValue(fields, "addTools")
	if !strings.Contains(tools, "https://coleta.exemplo.com/{id}") || !strings.Contains(tools, "POST") || !strings.Contains(tools, "headers: Authorization") {
		t.Fatalf("the card must show where the tool sends data: %q", tools)
	}
	if strings.Contains(tools, "Bearer segredo") {
		t.Fatal("header values stay off the card")
	}
	if cardValue(fields, "addKnowledgeBases") != "Tabela de preços" || cardValue(fields, "addMcpCollections") != "CRM externo" {
		t.Fatalf("linked bases and collections must be named: %+v", fields)
	}
}

func TestAnAgentCannotBeLinkedToAnotherWorkspacesKnowledge(t *testing.T) {
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, &fakeUpdateAgent{}, agentCardFixture())
	err := tool.(copilot.Validator).Validate(context.Background(), okContext(), map[string]interface{}{"id": "ag-1", "addKnowledgeBaseIds": []interface{}{"kb-x"}})
	if err == nil || !strings.Contains(err.Error(), "base de conhecimento desconhecida") {
		t.Fatalf("err = %v", err)
	}
	create := NewCreateAgentTool(&fakeCreateAgent{}, agentCardFixture())
	err = create.(copilot.Validator).Validate(context.Background(), okContext(), map[string]interface{}{
		"name": "Nova", "messagingPrompt": "p", "messagingModel": "m", "provider": "openrouter", "mcpCollectionIds": []interface{}{"mcp-other"},
	})
	if err == nil || !strings.Contains(err.Error(), "coleção MCP desconhecida") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeletingAnAgentNamesIt(t *testing.T) {
	a := &agent.Agent{ID: "ag-1", WorkspaceID: "ws-1", Name: "Atendente Maria"}
	fields := NewDeleteAgentTool(fakeGetAgent{a: a}, nil).(copilot.Describer).Describe(context.Background(), okContext(), map[string]interface{}{"id": "ag-1"})
	if cardValue(fields, "agent") != "Atendente Maria" || cardValue(fields, "risks") == "" {
		t.Fatalf("fields = %+v", fields)
	}
}
