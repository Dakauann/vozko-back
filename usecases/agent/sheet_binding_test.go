package agent_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/googlesheet"
	toolsdomain "vozko/domain/tools"
	"vozko/domain/workspace"
	tools_usecase "vozko/usecases/tools"
)

type sheetTool struct {
	toolsdomain.Handler
	workspaceSheets map[string]string
}

func (s sheetTool) ValidateBinding(_ context.Context, workspaceID string, config map[string]interface{}) error {
	id, _ := config["sheet_id"].(string)
	if s.workspaceSheets[id] != workspaceID {
		return googlesheet.ErrSheetNotFound
	}
	return nil
}

type sheetRegistry struct{ toolsdomain.Service }

func (sheetRegistry) Definitions() []toolsdomain.Definition {
	return []toolsdomain.Definition{{
		Name: tools_usecase.AddSheetRowToolName, BindingKey: "sheet_id", RequiresConfig: true, RequiredConfig: []string{"sheet_id"},
		ConfigSchema: map[string]toolsdomain.ConfigParameter{"sheet_id": {Type: "string", Required: true}},
	}}
}

func (sheetRegistry) Handler(string) (toolsdomain.Handler, bool) {
	return sheetTool{workspaceSheets: map[string]string{"sheet-a": "ws1", "sheet-b": "ws1", "foreign": "ws2"}}, true
}

type sheetViewers map[string]bool

func (v sheetViewers) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return resource == "google_sheets" && action == "read" && v[userID]
}

func sheetBinding(id string) agent.ToolBinding {
	return agent.ToolBinding{Name: tools_usecase.AddSheetRowToolName, Config: map[string]interface{}{"sheet_id": id}}
}

func TestBindingASheetNeedsToSeeSheetsAndASheetOfTheWorkspace(t *testing.T) {
	store := &agentStore{byID: map[string]*agent.Agent{}}
	create := NewCreateAgentUseCase(store, nil, sheetRegistry{}, nil, nil)
	create.(ToolBindingGuarded).GuardToolBindings(sheetViewers{"u-viewer": true})
	input := func(by string, bindings ...agent.ToolBinding) agent.CreateAgentInput {
		return agent.CreateAgentInput{WorkspaceID: "ws1", Name: "Nova", MessagingPrompt: "p", MessagingModel: "m", Provider: agent.AgentProviderAI,
			InternalTools: bindings, RequestedBy: workspace.Requester{UserID: by}}
	}
	if _, err := create.Execute(context.Background(), input("u-blind", sheetBinding("sheet-a"))); !errors.Is(err, workspace.ErrSheetBindingForbidden) {
		t.Fatalf("err = %v, want ErrSheetBindingForbidden", err)
	}
	if _, err := create.Execute(context.Background(), input("u-viewer", sheetBinding("foreign"))); !errors.Is(err, agent.ErrAgentInternalToolInvalid) {
		t.Fatalf("a sheet of another workspace was bound: %v", err)
	}
	if store.writes != 0 {
		t.Fatal("a refused agent was saved")
	}
	if _, err := create.Execute(context.Background(), input("u-viewer", sheetBinding("sheet-a"), sheetBinding("sheet-b"))); err != nil {
		t.Fatalf("two sheets of the workspace were refused: %v", err)
	}
}
