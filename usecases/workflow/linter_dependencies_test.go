package workflow_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/workflow"
)

type unreachableModels struct{}

func (unreachableModels) IsValidModel(context.Context, string) (bool, error) {
	return false, errors.New("model catalog unreachable")
}

func TestTheSharedLinterNamesEveryDependencyItStillNeeds(t *testing.T) {
	missing := NewActivateWorkflowUseCase(NewMockWorkflowRepository()).(*activateWorkflowUseCase).MissingDependencies()
	want := []string{"catalog", "templates", "agents", "media", "labels", "custom_fields", "stages", "deal_funnels", "departments", "members", "business_phones", "google_sheets", "model_catalog", "call_queues"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}
}

func TestAnAgentStepIsBlockedWhenItsModelCannotBeChecked(t *testing.T) {
	catalog := productionCatalog(t)
	uc := &activateWorkflowUseCase{repo: NewMockWorkflowRepository(), catalogFn: func() []workflow.NodeDefinition { return catalog }}
	uc.SetModelLookup(unreachableModels{})
	draft := &workflow.Workflow{WorkspaceID: "ws-1", Type: workflow.WorkflowTypeMessages, Graph: workflow.Graph{Nodes: []workflow.Node{{ID: "agent", Type: workflow.NodeTypeActionAIAgent, Config: map[string]interface{}{"source": "prompt", "model": "openai/gpt-5"}}}}}
	report, err := uc.Lint(context.Background(), draft)
	if err != nil || !reflect.DeepEqual(issueCodes(report.Issues)[workflow.LintValidationUnavailable], []string{"agent"}) {
		t.Fatalf("an unverifiable model passed: %v %+v", err, report.Issues)
	}
}

func TestAgentResourcesAreBlockedWhenTheirLookupIsNotAvailable(t *testing.T) {
	catalog := productionCatalog(t)
	uc := &activateWorkflowUseCase{repo: NewMockWorkflowRepository(), catalogFn: func() []workflow.NodeDefinition { return catalog }}
	for name, config := range map[string]map[string]interface{}{
		"model":           {"source": "prompt", "model": "openai/gpt-5"},
		"mcp collections": {"source": "prompt", "mcp_collection_ids": []interface{}{"mcp-1"}},
		"knowledge bases": {"source": "prompt", "knowledge_base_ids": []interface{}{"kb-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			draft := &workflow.Workflow{WorkspaceID: "ws-1", Type: workflow.WorkflowTypeMessages, Graph: workflow.Graph{Nodes: []workflow.Node{{ID: "agent", Type: workflow.NodeTypeActionAIAgent, Config: config}}}}
			report, err := uc.Lint(context.Background(), draft)
			if err != nil || !reflect.DeepEqual(issueCodes(report.Issues)[workflow.LintValidationUnavailable], []string{"agent"}) {
				t.Fatalf("an unverifiable resource passed: %v %+v", err, report.Issues)
			}
		})
	}
}
