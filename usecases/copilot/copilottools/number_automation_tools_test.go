package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/stage"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
)

const (
	numberID       = "1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	numberAgentID  = "2d3e4f5a-6b7c-4d8e-9f0a-1b2c3d4e5f6a"
	numberFlowID   = "3e4f5a6b-7c8d-4e9f-0a1b-2c3d4e5f6a7b"
	numberFunnelID = "4f5a6b7c-8d9e-4f0a-1b2c-3d4e5f6a7b8c"
)

var numberContext = copilot.Context{WorkspaceID: "ws-1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}

type numberAutomationStub struct {
	current wc.ReceptiveSettings
	saved   *wc.ReceptiveSettings
	err     error
}

func (s *numberAutomationStub) Get(string, string) (wc.ReceptiveSettings, error) {
	return s.current, s.err
}

func (s *numberAutomationStub) Update(_, _ string, settings wc.ReceptiveSettings) (wc.ReceptiveSettings, error) {
	s.saved = &settings
	return settings, s.err
}

type numberAgentsStub struct{}

func (numberAgentsStub) Execute(id string) (*agent.Agent, error) {
	if id == numberAgentID {
		return &agent.Agent{ID: id, WorkspaceID: numberContext.WorkspaceID, Name: "Atendente"}, nil
	}
	return nil, errors.New("not found")
}

type numberWorkflowsStub struct {
	workflow.ScopedWorkflowsUseCase
}

func (numberWorkflowsStub) Get(scope workflow.Scope, id string) (*workflow.Workflow, error) {
	if id == numberFlowID && scope.WorkspaceID == numberContext.WorkspaceID {
		return &workflow.Workflow{ID: id, Name: "Triagem"}, nil
	}
	return nil, errors.New("not found")
}

type numberFunnelsStub struct{}

func (numberFunnelsStub) Execute(string) ([]stage.FunnelStages, error) {
	return []stage.FunnelStages{{PipelineID: numberFunnelID, PipelineName: "Vendas"}}, nil
}

func numberDeps(numbers *numberAutomationStub) NumberAutomationDeps {
	return NumberAutomationDeps{Numbers: numbers, Agents: numberAgentsStub{}, Workflows: numberWorkflowsStub{}, Funnels: numberFunnelsStub{}}
}

func TestEloReadsWhoAnswersTheNumber(t *testing.T) {
	numbers := &numberAutomationStub{current: wc.ReceptiveSettings{AgentID: numberAgentID, EnableAgentResponses: true, PipelineID: numberFunnelID, EnableAnalysis: true}}
	result := NewNumberAutomationTool(numberDeps(numbers)).Execute(context.Background(), numberContext, map[string]interface{}{"business_phone_id": numberID})
	data, _ := result.Data.(map[string]interface{})
	if result.Status != copilot.StatusOK || data["answered_by"] != "agent" || data["agent"] != "Atendente" || data["pipeline"] != "Vendas" || data["analysis"] != true {
		t.Fatalf("result %+v", result)
	}
}

func TestEloTellsAGrantedWorkspaceWhoConfiguresTheNumber(t *testing.T) {
	numbers := &numberAutomationStub{err: wc.ErrReceptiveNotOwner}
	result := NewNumberAutomationTool(numberDeps(numbers)).Execute(context.Background(), numberContext, map[string]interface{}{"business_phone_id": numberID})
	if result.Status != copilot.StatusError || !strings.Contains(result.Message, "dono do número") {
		t.Fatalf("result %+v", result)
	}
}

func TestConfiguringTheNumberWaitsForApproval(t *testing.T) {
	if !NewConfigureNumberAutomationTool(numberDeps(&numberAutomationStub{})).Meta().Mutating {
		t.Fatal("changing who answers customers must wait for the user's approval")
	}
}

func TestEloHandsTheNumberToAWorkflowKeepingTheRest(t *testing.T) {
	numbers := &numberAutomationStub{current: wc.ReceptiveSettings{AgentID: numberAgentID, EnableAgentResponses: true, EnableAnalysis: true}}
	tool := NewConfigureNumberAutomationTool(numberDeps(numbers))
	args := map[string]interface{}{"business_phone_id": numberID, "answered_by": "workflow", "workflow_id": numberFlowID, "auto_staging": true}
	if err := tool.(copilot.Validator).Validate(context.Background(), numberContext, args); err != nil {
		t.Fatal(err)
	}
	if result := tool.Execute(context.Background(), numberContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	want := wc.ReceptiveSettings{AgentID: numberAgentID, WorkflowID: numberFlowID, EnableWorkflow: true, EnableAnalysis: true, EnableAutoStaging: true}
	if numbers.saved == nil || *numbers.saved != want {
		t.Fatalf("saved %+v", numbers.saved)
	}
}

func TestTheApprovalCardNamesWhatChanges(t *testing.T) {
	numbers := &numberAutomationStub{}
	args := map[string]interface{}{"business_phone_id": numberID, "answered_by": "agent", "agent_id": numberAgentID, "pipeline_id": numberFunnelID}
	fields := fieldMap(NewConfigureNumberAutomationTool(numberDeps(numbers)).(copilot.Describer).Describe(context.Background(), numberContext, args))
	if fields["agent"] != "Atendente" || fields["pipeline"] != "Vendas" {
		t.Fatalf("fields %+v", fields)
	}
}

func TestConfiguringRefusesUnknownOrMissingReferences(t *testing.T) {
	cases := []map[string]interface{}{
		{"business_phone_id": numberID, "answered_by": "agent"},
		{"business_phone_id": numberID, "answered_by": "agent", "agent_id": numberFlowID},
		{"business_phone_id": numberID, "answered_by": "workflow", "workflow_id": numberAgentID},
		{"business_phone_id": numberID, "answered_by": "none", "pipeline_id": numberAgentID},
		{"business_phone_id": numberID, "answered_by": "everyone"},
	}
	for _, args := range cases {
		numbers := &numberAutomationStub{}
		if err := NewConfigureNumberAutomationTool(numberDeps(numbers)).(copilot.Validator).Validate(context.Background(), numberContext, args); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

func TestConfiguringIsRefusedForAWorkspaceThatDoesNotOwnTheNumber(t *testing.T) {
	numbers := &numberAutomationStub{err: wc.ErrReceptiveNotOwner}
	args := map[string]interface{}{"business_phone_id": numberID, "answered_by": "none"}
	err := NewConfigureNumberAutomationTool(numberDeps(numbers)).(copilot.Validator).Validate(context.Background(), numberContext, args)
	if err == nil || !strings.Contains(err.Error(), "dono do número") {
		t.Fatalf("err %v", err)
	}
}
