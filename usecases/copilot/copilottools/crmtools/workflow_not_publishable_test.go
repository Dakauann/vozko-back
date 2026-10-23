package crmtools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/workflow"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func TestActivatingADraftWithProblemsTellsTheAssistantEveryBlockingProblem(t *testing.T) {
	report := workflow.LintGraph(&workflow.Graph{Nodes: []workflow.Node{{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}}}}, workflow.WorkflowTypeMessages, nil, nil, nil)
	blocked := workflow.NotPublishableFrom(report, []error{fmt.Errorf("%w: node %q label_id %q", workflow.ErrNodeInvalidLabelID, "lbl", "gone")})

	res := NewActivateWorkflowTool(&fakeScopedWorkflows{err: blocked}).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"workflow_id": knownWorkflow})

	if res.Status != copilot.StatusError {
		t.Fatalf("status = %s", res.Status)
	}
	for _, issue := range report.Blocking() {
		if !strings.Contains(res.Message, issue.Message) {
			t.Errorf("message %q misses %q", res.Message, issue.Message)
		}
	}
	if !strings.Contains(res.Message, `label_id "gone"`) {
		t.Errorf("message %q misses the label problem", res.Message)
	}
}
