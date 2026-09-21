package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
)

func workflowWithID(id string) *workflow.Workflow {
	return &workflow.Workflow{ID: id}
}

func TestAccountLinkSelectsOnlyTheLinkedWorkflow(t *testing.T) {
	te := &triggerEvaluator{}
	event := workflow.TriggerEvent{
		TriggerType: workflow.TriggerMessageReceived,
		Data:        map[string]interface{}{"account_workflow_id": "wf-linked"},
	}

	if !te.matchesTriggerConfig(workflowWithID("wf-linked"), event) {
		t.Error("the linked workflow must run")
	}
	if te.matchesTriggerConfig(workflowWithID("wf-other"), event) {
		t.Error("a workflow the account is NOT linked to must not run")
	}
}

func TestNoAccountLinkLeavesEveryWorkflowEligible(t *testing.T) {
	te := &triggerEvaluator{}

	for _, data := range []map[string]interface{}{
		{},
		{"account_workflow_id": ""},
	} {
		event := workflow.TriggerEvent{TriggerType: workflow.TriggerMessageReceived, Data: data}
		if !te.matchesTriggerConfig(workflowWithID("wf-any"), event) {
			t.Errorf("data=%v must not filter anything out", data)
		}
	}
}

func TestCampaignLinkStillFilters(t *testing.T) {
	te := &triggerEvaluator{}
	event := workflow.TriggerEvent{
		TriggerType: workflow.TriggerMessageReceived,
		Data:        map[string]interface{}{"campaign_workflow_id": "wf-campaign"},
	}

	if te.matchesTriggerConfig(workflowWithID("wf-other"), event) {
		t.Error("a workflow the campaign is not linked to must not run")
	}
	if !te.matchesTriggerConfig(workflowWithID("wf-campaign"), event) {
		t.Error("the campaign's own workflow must run")
	}
}
