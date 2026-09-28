package tools_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/opportunity"
	"vozko/domain/stage"
	"vozko/domain/tools"
	opportunity_usecase "vozko/usecases/opportunity"
)

type fakeDeals struct {
	stages   []*stage.Stage
	deals    opportunity.EntryDeals
	manageFn func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error)
	commands []opportunity_usecase.EntryCommand
}

func (f *fakeDeals) PipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if pipelineID != "pipe1" {
		return nil, opportunity_usecase.ErrNotOpportunityPipeline
	}
	return f.stages, nil
}

func (f *fakeDeals) DealsForEntry(workspaceID, pipelineID, entryID, entryType string) (opportunity.EntryDeals, error) {
	return f.deals, nil
}

func (f *fakeDeals) ManageForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
	f.commands = append(f.commands, cmd)
	if f.manageFn != nil {
		return f.manageFn(cmd)
	}
	o := &opportunity.Opportunity{ID: "deal-1", Title: cmd.Title, StageID: "stage-new", Currency: "BRL", Status: opportunity.StatusOpen}
	if cmd.ValueCents != nil {
		o.ValueCents = *cmd.ValueCents
	}
	switch cmd.Action {
	case opportunity_usecase.EntryWin:
		o.StageID, o.Status = "stage-won", opportunity.StatusWon
	case opportunity_usecase.EntryMove:
		o.StageID = cmd.StageID
	}
	return &opportunity_usecase.EntryResult{Opportunity: o, Created: true}, nil
}

func dealStages() []*stage.Stage {
	return []*stage.Stage{
		{ID: "stage-new", Name: "Novo", PipelineID: "pipe1", IsInitial: true},
		{ID: "stage-proposal", Name: "Proposta", PipelineID: "pipe1"},
		{ID: "stage-won", Name: "Ganho", PipelineID: "pipe1", IsWon: true},
		{ID: "stage-lost", Name: "Perdido", PipelineID: "pipe1", IsLost: true},
	}
}

func dealTool(deals *fakeDeals) *manageOpportunityTool {
	if deals.stages == nil {
		deals.stages = dealStages()
	}
	return NewManageOpportunityTool(deals).(*manageOpportunityTool)
}

func conversationConfig(extra map[string]interface{}) map[string]interface{} {
	config := map[string]interface{}{
		"__workspace_id": "ws1",
		"__entry_id":     "entry-1",
		"__entry_type":   "whatsapp",
		"__agent_id":     "agent-1",
		"__lead_id":      "lead-1",
		"pipeline_id":    "pipe1",
	}
	for k, v := range extra {
		config[k] = v
	}
	return config
}

func run(t *testing.T, tool *manageOpportunityTool, config, params map[string]interface{}) tools.ExecutionResult {
	t.Helper()
	result, err := tool.ExecuteWithConfig(context.Background(), config, params)
	if err != nil {
		t.Fatalf("ExecuteWithConfig() error = %v", err)
	}
	return result
}

func TestManageOpportunityCreatesTheDealAsTheAgent(t *testing.T) {
	deals := &fakeDeals{}
	result := run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{
		"action": "create", "title": "Plano Pro", "value": 1500.5,
	})
	if result.IsError {
		t.Fatalf("create refused: %v", result.Result)
	}
	cmd := deals.commands[0]
	if cmd.Actor != "ai:agent-1" || cmd.EntryID != "entry-1" || cmd.EntryType != "whatsapp" || cmd.LeadID != "lead-1" || cmd.PipelineID != "pipe1" {
		t.Fatalf("command = %+v", cmd)
	}
	if cmd.ValueCents == nil || *cmd.ValueCents != 150050 {
		t.Fatalf("value = %v, want 150050 cents", cmd.ValueCents)
	}
	if result.ContextUpdateText == "" {
		t.Fatalf("the model was not told the deal's new state")
	}
}

func TestManageOpportunityCannotWinUnlessTheAdminAllowedIt(t *testing.T) {
	deals := &fakeDeals{}
	result := run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{"action": "win", "value": 100.0})
	if !result.IsError || len(deals.commands) != 0 {
		t.Fatalf("win without permission = %+v, commands %d", result, len(deals.commands))
	}

	allowed := conversationConfig(map[string]interface{}{"allowed_actions": []interface{}{"win"}})
	result = run(t, dealTool(deals), allowed, map[string]interface{}{"action": "win", "value": 100.0})
	if result.IsError || deals.commands[0].Action != opportunity_usecase.EntryWin {
		t.Fatalf("allowed win = %+v", result)
	}
}

func TestManageOpportunityOnlyGrantsTheConfiguredActions(t *testing.T) {
	deals := &fakeDeals{}
	config := conversationConfig(map[string]interface{}{"allowed_actions": []interface{}{"update_value"}})
	result := run(t, dealTool(deals), config, map[string]interface{}{"action": "create", "title": "X"})
	if !result.IsError || len(deals.commands) != 0 {
		t.Fatalf("create outside the allowed actions = %+v", result)
	}
}

func TestManageOpportunityMovesOnlyToOpenStagesByName(t *testing.T) {
	deals := &fakeDeals{}
	tool := dealTool(deals)
	result := run(t, tool, conversationConfig(nil), map[string]interface{}{"action": "move", "stage": "proposta"})
	if result.IsError || deals.commands[0].StageID != "stage-proposal" {
		t.Fatalf("move = %+v, commands %+v", result, deals.commands)
	}

	result = run(t, tool, conversationConfig(nil), map[string]interface{}{"action": "move", "stage": "Ganho"})
	if !result.IsError || len(deals.commands) != 1 {
		t.Fatalf("moving onto a won stage bypassed the win permission: %+v", result)
	}
}

func TestManageOpportunityLosesWithTheGivenReason(t *testing.T) {
	deals := &fakeDeals{}
	run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{"action": "lose", "lost_reason": " Achou caro "})
	if deals.commands[0].LostReasonID != "Achou caro" {
		t.Fatalf("lost reason = %q", deals.commands[0].LostReasonID)
	}
}

func TestManageOpportunityListsTheConversationsDeals(t *testing.T) {
	deals := &fakeDeals{deals: opportunity.EntryDeals{
		{ID: "deal-1", Title: "Plano Pro", StageID: "stage-proposal", ValueCents: 7900, Currency: "BRL", Status: opportunity.StatusOpen},
		{ID: "deal-2", Title: "Contrato anual", StageID: "stage-won", ValueCents: 120000, Currency: "BRL", Status: opportunity.StatusWon},
	}}
	result := run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{"action": "get"})
	text, _ := result.Result.(string)
	for _, want := range []string{"deal-1", "Plano Pro", "Proposta", "79,00", "deal-2", "Contrato anual", "ganho"} {
		if result.IsError || !strings.Contains(text, want) {
			t.Fatalf("get is missing %q: %+v", want, result)
		}
	}

	result = run(t, dealTool(&fakeDeals{}), conversationConfig(nil), map[string]interface{}{"action": "get"})
	if result.IsError {
		t.Fatalf("a conversation without a deal is an answer, not an error: %+v", result)
	}
}

func TestManageOpportunityTargetsTheDealTheModelChose(t *testing.T) {
	deals := &fakeDeals{}
	run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{"action": "update_value", "value": 10.0, "opportunity_id": " deal-2 "})
	if deals.commands[0].OpportunityID != "deal-2" {
		t.Fatalf("opportunity id = %q", deals.commands[0].OpportunityID)
	}
}

func TestManageOpportunityHandsTheListBackWhenItCannotGuess(t *testing.T) {
	deals := &fakeDeals{
		deals: opportunity.EntryDeals{
			{ID: "deal-1", Title: "Plano Pro", StageID: "stage-new", Currency: "BRL", Status: opportunity.StatusOpen},
			{ID: "deal-2", Title: "Plano Plus", StageID: "stage-new", Currency: "BRL", Status: opportunity.StatusOpen},
		},
		manageFn: func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
			return nil, opportunity.ErrAmbiguousDeal
		},
	}
	result := run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{"action": "update_value", "value": 10.0})
	text, _ := result.Result.(string)
	if !result.IsError || !strings.Contains(text, "opportunity_id") || !strings.Contains(text, "deal-1") || !strings.Contains(text, "deal-2") {
		t.Fatalf("ambiguous = %+v", result)
	}
}

func TestManageOpportunityOpensAnotherDealOnlyWhenTheAdminAllowedIt(t *testing.T) {
	deals := &fakeDeals{}
	result := run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{"action": "create_new", "title": "Contrato 2"})
	if !result.IsError || len(deals.commands) != 0 {
		t.Fatalf("create_new without permission = %+v", result)
	}
	allowed := conversationConfig(map[string]interface{}{"allowed_actions": []interface{}{"create_new"}})
	result = run(t, dealTool(deals), allowed, map[string]interface{}{"action": "create_new", "title": "Contrato 2"})
	if result.IsError || deals.commands[0].Action != opportunity_usecase.EntryCreateNew {
		t.Fatalf("allowed create_new = %+v", result)
	}
}

func TestManageOpportunityRefusesWithoutAConversationOrAnAgent(t *testing.T) {
	for name, key := range map[string]string{"no conversation": "__entry_id", "no agent": "__agent_id", "no pipeline": "pipeline_id"} {
		t.Run(name, func(t *testing.T) {
			deals := &fakeDeals{}
			config := conversationConfig(nil)
			delete(config, key)
			result := run(t, dealTool(deals), config, map[string]interface{}{"action": "create", "title": "X"})
			if !result.IsError || len(deals.commands) != 0 {
				t.Fatalf("%s = %+v", name, result)
			}
		})
	}
}

func TestManageOpportunityRefusesAValueThatIsNotANumber(t *testing.T) {
	deals := &fakeDeals{}
	result := run(t, dealTool(deals), conversationConfig(nil), map[string]interface{}{"action": "update_value", "value": "mil reais"})
	if !result.IsError || len(deals.commands) != 0 {
		t.Fatalf("value %q = %+v", "mil reais", result)
	}
}

func TestManageOpportunityExplainsBusinessRefusals(t *testing.T) {
	deals := &fakeDeals{manageFn: func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
		return nil, opportunity.ErrWonWithoutValue
	}}
	config := conversationConfig(map[string]interface{}{"allowed_actions": []interface{}{"win"}})
	result := run(t, dealTool(deals), config, map[string]interface{}{"action": "win"})
	text, _ := result.Result.(string)
	if !result.IsError || !strings.Contains(text, "value") {
		t.Fatalf("won without value = %+v", result)
	}

	deals.manageFn = func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
		return nil, errors.New("connection reset")
	}
	result = run(t, dealTool(deals), config, map[string]interface{}{"action": "win", "value": 10.0})
	text, _ = result.Result.(string)
	if !result.IsError || strings.Contains(text, "connection reset") {
		t.Fatalf("an infrastructure failure leaked to the model: %+v", result)
	}
}

func TestManageOpportunityDefinitionListsOnlyWhatTheAgentMayDo(t *testing.T) {
	tool := dealTool(&fakeDeals{})
	def := tool.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws1", Config: map[string]interface{}{"pipeline_id": "pipe1"}})

	actions := strings.Join(def.Parameters["action"].Enum, ",")
	if actions != "get,create,update_value,move,lose" {
		t.Fatalf("action enum = %s", actions)
	}
	stages := strings.Join(def.Parameters["stage"].Enum, ",")
	if stages != "Novo,Proposta" {
		t.Fatalf("stage enum = %s, want only the open stages", stages)
	}

	withWin := tool.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws1", Config: map[string]interface{}{
		"pipeline_id": "pipe1", "allowed_actions": []interface{}{"win", "bogus"},
	}})
	if got := strings.Join(withWin.Parameters["action"].Enum, ","); got != "get,win" {
		t.Fatalf("action enum = %s, want get,win", got)
	}
	if _, found := withWin.Parameters["stage"]; found {
		t.Fatalf("stage offered to an agent that cannot move")
	}
}

func TestManageOpportunityDefinitionIsAConfiguredMessagingAction(t *testing.T) {
	def := dealTool(&fakeDeals{}).Definition()
	if !def.RequiresConfig || def.Category != tools.CategoryAgentAction {
		t.Fatalf("definition = %+v", def)
	}
	if def.IsVisibleIn(tools.VisibilityPostConversation) {
		t.Fatalf("post-conversation runs cannot credit the agent, so the tool must not run there")
	}
	if def.ConfigSchema["pipeline_id"].OptionsSource != "opportunity_pipelines" {
		t.Fatalf("pipeline picker = %+v", def.ConfigSchema["pipeline_id"])
	}
	if err := def.ValidateConfig(map[string]interface{}{"allowed_actions": []interface{}{"win"}}); err == nil {
		t.Fatalf("a config without a pipeline was accepted")
	}
}

func TestManageOpportunityWordsEveryRefusal(t *testing.T) {
	for _, refusal := range opportunity_usecase.Refusals() {
		if toolRefusals[refusal] == "" {
			t.Fatalf("refusal %q has no message for the model", refusal)
		}
	}
}
