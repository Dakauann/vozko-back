package node_executors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/opportunity"
	"vozko/domain/workflow"
	opportunity_usecase "vozko/usecases/opportunity"
)

type dealDeskStub struct {
	deals    opportunity.EntryDeals
	readErr  error
	manageFn func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error)
	commands []opportunity_usecase.EntryCommand
}

func (d *dealDeskStub) DealsForEntry(_, _, _, _ string) (opportunity.EntryDeals, error) {
	if d.readErr != nil {
		return nil, d.readErr
	}
	return d.deals, nil
}

func (d *dealDeskStub) ManageForEntry(_ string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
	d.commands = append(d.commands, cmd)
	if d.manageFn != nil {
		return d.manageFn(cmd)
	}
	o := &opportunity.Opportunity{ID: "deal-1", Title: cmd.Title, StageID: "st-deal-new", Status: opportunity.StatusOpen, Currency: "BRL"}
	if cmd.ValueCents != nil {
		o.ValueCents = *cmd.ValueCents
	}
	if cmd.Action == opportunity_usecase.EntryWin {
		o.Status, o.StageID = opportunity.StatusWon, "st-deal-won"
	}
	return &opportunity_usecase.EntryResult{Opportunity: o, Created: true}, nil
}

func manageDeal(t *testing.T, desk *dealDeskStub, config map[string]interface{}) *workflow.NodeResult {
	t.Helper()
	ctx := stageNodeCtx(workflow.NodeTypeActionManageOpportunity, config, moveEdges())
	ctx.State.Set("_last_value", 1500.5)
	ctx.State.Set("_last_opportunity_id", "deal-9")
	result, err := NewManageOpportunityExecutor(desk).Execute(ctx)
	require.NoError(t, err)
	return result
}

func TestManageOpportunityNodeActsAsTheWorkflow(t *testing.T) {
	desk := &dealDeskStub{}
	result := manageDeal(t, desk, map[string]interface{}{
		"pipeline_id": "pl-deals", "action": "win", "title": "Plano Pro", "value": "{{last.value}}",
	})

	require.Equal(t, "ok", result.NextNodeID)
	require.Len(t, desk.commands, 1)
	cmd := desk.commands[0]
	require.Equal(t, "workflow:wf-1", cmd.Actor)
	require.Equal(t, "e-1", cmd.EntryID)
	require.Equal(t, "unofficial_whatsapp", cmd.EntryType)
	require.Equal(t, opportunity_usecase.EntryWin, cmd.Action)
	require.Empty(t, cmd.OpportunityID)
	require.NotNil(t, cmd.ValueCents)
	require.EqualValues(t, 150050, *cmd.ValueCents)
	require.Equal(t, true, result.Output["success"])
	require.Equal(t, "deal-1", result.Output["opportunity_id"])
	require.Equal(t, "won", result.Output["status"])
}

func TestManageOpportunityNodeTargetsTheDealFromAVariable(t *testing.T) {
	desk := &dealDeskStub{}
	manageDeal(t, desk, map[string]interface{}{
		"pipeline_id": "pl-deals", "action": "update_value", "value": "10", "opportunity_id": "{{last.opportunity_id}}",
	})
	require.Len(t, desk.commands, 1)
	require.Equal(t, "deal-9", desk.commands[0].OpportunityID)
}

func TestManageOpportunityNodeCreatesASecondDealOnRequest(t *testing.T) {
	desk := &dealDeskStub{}
	manageDeal(t, desk, map[string]interface{}{"pipeline_id": "pl-deals", "action": "create_new", "title": "Contrato 2"})
	require.Len(t, desk.commands, 1)
	require.Equal(t, opportunity_usecase.EntryCreateNew, desk.commands[0].Action)
}

func TestManageOpportunityNodeRoutesAnAmbiguousDealToErro(t *testing.T) {
	desk := &dealDeskStub{manageFn: func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
		return nil, opportunity.ErrAmbiguousDeal
	}}
	result := manageDeal(t, desk, map[string]interface{}{"pipeline_id": "pl-deals", "action": "update_value", "value": "10"})
	require.Equal(t, "fail", result.NextNodeID)
	require.Contains(t, result.Output["error"], "ID do negócio")
}

func TestManageOpportunityNodeRoutesARefusalToErro(t *testing.T) {
	desk := &dealDeskStub{manageFn: func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
		return nil, opportunity.ErrWonWithoutValue
	}}
	result := manageDeal(t, desk, map[string]interface{}{"pipeline_id": "pl-deals", "action": "win"})
	require.Equal(t, "fail", result.NextNodeID)
	require.Equal(t, false, result.Output["success"])
	require.Contains(t, result.Output["error"], "Valor")
}

func TestManageOpportunityNodeNeverFallsThroughToTheFirstEdge(t *testing.T) {
	desk := &dealDeskStub{manageFn: func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
		return nil, opportunity_usecase.ErrNoOpenDeal
	}}
	ctx := stageNodeCtx(workflow.NodeTypeActionManageOpportunity,
		map[string]interface{}{"pipeline_id": "pl-deals", "action": "lose", "lost_reason": "preço"},
		[]workflow.Edge{{Source: "n1", Target: "ok", Label: "sucesso"}})
	result, err := NewManageOpportunityExecutor(desk).Execute(ctx)
	require.NoError(t, err)
	require.Empty(t, result.NextNodeID)
}

func TestManageOpportunityNodeRefusesAValueThatIsNotANumber(t *testing.T) {
	desk := &dealDeskStub{}
	result := manageDeal(t, desk, map[string]interface{}{"pipeline_id": "pl-deals", "action": "update_value", "value": "mil reais"})
	require.Equal(t, "fail", result.NextNodeID)
	require.Empty(t, desk.commands)
}

func TestManageOpportunityNodeReportsAnInfrastructureFailure(t *testing.T) {
	desk := &dealDeskStub{manageFn: func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
		return nil, errors.New("connection reset")
	}}
	result := manageDeal(t, desk, map[string]interface{}{"pipeline_id": "pl-deals", "action": "create", "title": "X"})
	require.Equal(t, "fail", result.NextNodeID)
	require.Contains(t, result.Output["error"], "connection reset")
}

func TestManageOpportunityNodeNeedsItsFunnelAndAction(t *testing.T) {
	for name, config := range map[string]map[string]interface{}{
		"no funnel":      {"action": "create"},
		"unknown action": {"pipeline_id": "pl-deals", "action": "delete"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := stageNodeCtx(workflow.NodeTypeActionManageOpportunity, config, moveEdges())
			_, err := NewManageOpportunityExecutor(&dealDeskStub{}).Execute(ctx)
			require.ErrorIs(t, err, workflow.ErrNodeConfigMissing)
		})
	}
}

func TestManageOpportunityNodeWithoutDealsFailsClosed(t *testing.T) {
	ctx := stageNodeCtx(workflow.NodeTypeActionManageOpportunity, map[string]interface{}{"pipeline_id": "pl-deals", "action": "create"}, moveEdges())
	result, err := NewManageOpportunityExecutor(nil).Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "fail", result.NextNodeID)
}

func TestManageOpportunityNodeWordsEveryRefusal(t *testing.T) {
	for _, refusal := range opportunity_usecase.Refusals() {
		require.NotEmpty(t, nodeRefusals[refusal], "refusal %q has no message for the builder", refusal)
	}
}

func TestManageOpportunityNodeRequiresOnlyWhatEachActionUses(t *testing.T) {
	catalog := []workflow.NodeDefinition{NewManageOpportunityExecutor(&dealDeskStub{}).Definition()}
	validate := func(config map[string]interface{}) error {
		config["pipeline_id"] = "pl-deals"
		graph := &workflow.Graph{Nodes: []workflow.Node{{ID: "n1", Type: workflow.NodeTypeActionManageOpportunity, Config: config}}}
		return workflow.ValidateNodeConfigs(graph, catalog)
	}
	require.ErrorIs(t, validate(map[string]interface{}{"action": "move"}), workflow.ErrNodeMissingRequiredField)
	require.ErrorIs(t, validate(map[string]interface{}{"action": "lose"}), workflow.ErrNodeMissingRequiredField)
	require.ErrorIs(t, validate(map[string]interface{}{"action": "update_value"}), workflow.ErrNodeMissingRequiredField)
	require.NoError(t, validate(map[string]interface{}{"action": "move", "stage_id": "st-deal-new"}))
	require.NoError(t, validate(map[string]interface{}{"action": "win"}))
	require.NoError(t, validate(map[string]interface{}{"action": "create_new"}))
}

func TestManageOpportunityNodeHidesTheDealIDWhenCreatingANewOne(t *testing.T) {
	def := NewManageOpportunityExecutor(&dealDeskStub{}).Definition()
	for _, field := range def.ConfigSchema {
		if field.Key != "opportunity_id" {
			continue
		}
		require.False(t, field.VisibleFor(map[string]interface{}{"action": "create_new"}))
		require.True(t, field.VisibleFor(map[string]interface{}{"action": "move"}))
		return
	}
	t.Fatal("the node has no deal id field")
}

func checkDeal(t *testing.T, desk *dealDeskStub, config map[string]interface{}) *workflow.NodeResult {
	t.Helper()
	result, err := NewCheckOpportunityExecutor(desk).Execute(stageNodeCtx(workflow.NodeTypeConditionCheckOpportunity, config, checkEdges()))
	require.NoError(t, err)
	return result
}

var (
	wonDeal   = &opportunity.Opportunity{ID: "deal-1", Status: opportunity.StatusWon, StageID: "st-deal-won", ValueCents: 7900}
	openDeal  = &opportunity.Opportunity{ID: "deal-2", Status: opportunity.StatusOpen, StageID: "st-deal-new", ValueCents: 1000}
	otherOpen = &opportunity.Opportunity{ID: "deal-3", Status: opportunity.StatusOpen, StageID: "st-deal-new", ValueCents: 2000}
)

func TestCheckOpportunityNodeAnswersItsQuestions(t *testing.T) {
	won := opportunity.EntryDeals{wonDeal}
	cases := []struct {
		name   string
		desk   *dealDeskStub
		config map[string]interface{}
		want   string
	}{
		{"has a deal", &dealDeskStub{deals: won}, map[string]interface{}{"check": "exists"}, "yes"},
		{"has no deal", &dealDeskStub{}, map[string]interface{}{"check": "exists"}, "no"},
		{"status matches", &dealDeskStub{deals: won}, map[string]interface{}{"check": "status", "status": "won"}, "yes"},
		{"status differs", &dealDeskStub{deals: won}, map[string]interface{}{"check": "status", "status": "open"}, "no"},
		{"no deal has no status", &dealDeskStub{}, map[string]interface{}{"check": "status", "status": "open"}, "no"},
		{"on the stage", &dealDeskStub{deals: won}, map[string]interface{}{"check": "stage", "stage_id": "st-deal-won"}, "yes"},
		{"on another stage", &dealDeskStub{deals: won}, map[string]interface{}{"check": "stage", "stage_id": "st-deal-new"}, "no"},
		{"one open deal", &dealDeskStub{deals: opportunity.EntryDeals{openDeal, wonDeal}}, map[string]interface{}{"check": "several_open"}, "no"},
		{"several open deals", &dealDeskStub{deals: opportunity.EntryDeals{openDeal, otherOpen}}, map[string]interface{}{"check": "several_open"}, "yes"},
		{"the chosen deal by id", &dealDeskStub{deals: opportunity.EntryDeals{openDeal, otherOpen}}, map[string]interface{}{"check": "status", "status": "open", "opportunity_id": "deal-3"}, "yes"},
		{"a deal of another conversation", &dealDeskStub{deals: won}, map[string]interface{}{"check": "exists", "opportunity_id": "elsewhere"}, "no"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.config["pipeline_id"] = "pl-deals"
			require.Equal(t, tc.want, checkDeal(t, tc.desk, tc.config).NextNodeID)
		})
	}
}

func TestCheckOpportunityNodeRefusesToGuessBetweenOpenDeals(t *testing.T) {
	desk := &dealDeskStub{deals: opportunity.EntryDeals{openDeal, otherOpen}}
	ctx := stageNodeCtx(workflow.NodeTypeConditionCheckOpportunity, map[string]interface{}{"pipeline_id": "pl-deals", "check": "status", "status": "open"}, checkEdges())
	_, err := NewCheckOpportunityExecutor(desk).Execute(ctx)
	require.ErrorIs(t, err, opportunity.ErrAmbiguousDeal)
}

func TestCheckOpportunityNodeExposesTheDealAndTheList(t *testing.T) {
	desk := &dealDeskStub{deals: opportunity.EntryDeals{openDeal, wonDeal}}
	out := checkDeal(t, desk, map[string]interface{}{"pipeline_id": "pl-deals", "check": "exists"}).Output
	require.Equal(t, "deal-2", out["opportunity_id"])
	require.Equal(t, "open", out["status"])
	require.EqualValues(t, 1000, out["value_cents"])
	require.Equal(t, 1, out["open_count"])
	list, ok := out["opportunities"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, list, 2)
	require.Equal(t, "deal-1", list[1]["opportunity_id"])
}

func TestCheckOpportunityNodeListsEvenWhenAmbiguous(t *testing.T) {
	desk := &dealDeskStub{deals: opportunity.EntryDeals{openDeal, otherOpen}}
	out := checkDeal(t, desk, map[string]interface{}{"pipeline_id": "pl-deals", "check": "exists"}).Output
	require.Equal(t, 2, out["open_count"])
	require.Equal(t, "", out["opportunity_id"])
	require.Len(t, out["opportunities"], 2)
}

func TestCheckOpportunityNodeRetriesAFailedRead(t *testing.T) {
	desk := &dealDeskStub{readErr: errors.New("connection reset")}
	ctx := stageNodeCtx(workflow.NodeTypeConditionCheckOpportunity, map[string]interface{}{"pipeline_id": "pl-deals", "check": "exists"}, checkEdges())
	_, err := NewCheckOpportunityExecutor(desk).Execute(ctx)
	require.Error(t, err)
}

func TestCheckOpportunityNodeNeedsACompleteQuestion(t *testing.T) {
	for name, config := range map[string]map[string]interface{}{
		"no funnel":         {"check": "exists"},
		"unknown check":     {"pipeline_id": "pl-deals", "check": "value"},
		"status to compare": {"pipeline_id": "pl-deals", "check": "status"},
		"stage to compare":  {"pipeline_id": "pl-deals", "check": "stage"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := stageNodeCtx(workflow.NodeTypeConditionCheckOpportunity, config, checkEdges())
			_, err := NewCheckOpportunityExecutor(&dealDeskStub{}).Execute(ctx)
			require.ErrorIs(t, err, workflow.ErrNodeConfigMissing)
		})
	}
}

func TestOpportunityNodesPickDealFunnelsAndStages(t *testing.T) {
	for _, def := range []workflow.NodeDefinition{
		NewManageOpportunityExecutor(&dealDeskStub{}).Definition(),
		NewCheckOpportunityExecutor(&dealDeskStub{}).Definition(),
	} {
		sources := map[string]string{}
		for _, field := range def.ConfigSchema {
			sources[field.Key] = field.OptionsSource
		}
		require.Equal(t, "opportunity_pipelines", sources["pipeline_id"], def.Type)
		require.Equal(t, "opportunity_stages", sources["stage_id"], def.Type)
	}
}
