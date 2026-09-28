package tools_usecase

import (
	"errors"
	"testing"

	"vozko/domain/tools"
	opportunity_usecase "vozko/usecases/opportunity"
)

type ownersStub struct {
	owner string
	err   error
}

func (o ownersStub) ConversationOwner(workspaceID, entryID, entryType string) (string, error) {
	return o.owner, o.err
}

func autoDealTool(deals *fakeDeals, owners ownersStub) *manageOpportunityTool {
	if deals.stages == nil {
		deals.stages = dealStages()
	}
	return NewAutoManageOpportunityTool(deals, owners).(*manageOpportunityTool)
}

func analysisConfig(agentID string) map[string]interface{} {
	return map[string]interface{}{
		"__workspace_id": "ws1",
		"__entry_id":     "entry-1",
		"__entry_type":   "instagram",
		"__lead_id":      "lead-1",
		"__agent_id":     agentID,
		"pipeline_id":    "pipe1",
	}
}

func TestAutoDealCreditsTheConversationsOwner(t *testing.T) {
	deals := &fakeDeals{}
	result := run(t, autoDealTool(deals, ownersStub{owner: "user-7"}), analysisConfig("agent-1"), map[string]interface{}{"action": "create", "title": "Plano Pro"})
	if result.IsError {
		t.Fatalf("create refused: %v", result.Result)
	}
	cmd := deals.commands[0]
	if cmd.Actor != "system" || cmd.Owner != "user-7" || cmd.EntryType != "instagram" || cmd.PipelineID != "pipe1" {
		t.Fatalf("command = %+v", cmd)
	}
}

func TestAutoDealFallsBackToTheChannelAgent(t *testing.T) {
	deals := &fakeDeals{}
	run(t, autoDealTool(deals, ownersStub{}), analysisConfig("agent-1"), map[string]interface{}{"action": "create", "title": "X"})
	if deals.commands[0].Owner != "ai:agent-1" {
		t.Fatalf("owner = %q", deals.commands[0].Owner)
	}
}

func TestAutoDealRefusesWithoutAnyoneToCredit(t *testing.T) {
	for name, owners := range map[string]ownersStub{
		"no owner and no agent": {},
		"owner lookup failed":   {err: errors.New("db down")},
	} {
		t.Run(name, func(t *testing.T) {
			deals := &fakeDeals{}
			agent := ""
			if owners.err != nil {
				agent = "agent-1"
			}
			result := run(t, autoDealTool(deals, owners), analysisConfig(agent), map[string]interface{}{"action": "create", "title": "X"})
			if !result.IsError || len(deals.commands) != 0 {
				t.Fatalf("%s = %+v", name, result)
			}
		})
	}
}

func TestAutoDealMayCloseAndOpenAnotherDeal(t *testing.T) {
	deals := &fakeDeals{}
	tool := autoDealTool(deals, ownersStub{owner: "user-7"})
	for _, action := range []string{"win", "lose", "create_new"} {
		result := run(t, tool, analysisConfig(""), map[string]interface{}{"action": action, "value": 10.0, "lost_reason": "preço", "title": "X"})
		if result.IsError {
			t.Fatalf("%s refused: %v", action, result.Result)
		}
	}
	if deals.commands[0].Action != opportunity_usecase.EntryWin {
		t.Fatalf("first action = %s", deals.commands[0].Action)
	}
}

func TestAutoDealIsOnlyAvailableToTheAnalysis(t *testing.T) {
	def := autoDealTool(&fakeDeals{}, ownersStub{}).Definition()
	if def.Name != AutoManageOpportunityToolName || def.Name == ManageOpportunityToolName {
		t.Fatalf("name = %q", def.Name)
	}
	if !def.IsVisibleIn(tools.VisibilityAnalysis) || def.IsVisibleIn(tools.VisibilityMessaging) || def.IsVisibleIn(tools.VisibilityPostConversation) {
		t.Fatalf("visibility = %v", def.Visibility)
	}
	if def.RequiresConfig || len(def.ConfigSchema) != 0 {
		t.Fatal("the analysis tool is configured by the server, never by an admin")
	}
}
