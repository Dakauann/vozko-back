package tools_usecase

import (
	"slices"
	"testing"

	"vozko/domain/tools"
)

func requiresCurrentPosition(t *testing.T, name string, def tools.Definition) {
	t.Helper()
	if _, ok := def.Parameters[tools.CurrentPositionParameter]; !ok {
		t.Errorf("%s does not ask for the customer's current position", name)
	}
	if !slices.Contains(def.Required, tools.CurrentPositionParameter) {
		t.Errorf("%s must require the current position before deciding", name)
	}
}

func TestStageAndDealDecisionsStateTheCurrentPositionFirst(t *testing.T) {
	stageTool := &manageEntryStageTool{stageRepo: &stubStageRepo{stages: stagesFixture()}}
	requiresCurrentPosition(t, "stage tool", stageTool.Definition())
	requiresCurrentPosition(t, "stage tool with its funnel", stageTool.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1"}))

	agentDeals := dealTool(&fakeDeals{})
	requiresCurrentPosition(t, "agent opportunity tool", agentDeals.Definition())
	requiresCurrentPosition(t, "agent opportunity tool in context", agentDeals.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws1"}))

	autoDeals := autoDealTool(&fakeDeals{}, ownersStub{})
	requiresCurrentPosition(t, "automatic opportunity tool", autoDeals.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws1", Config: analysisConfig("")}))
}
