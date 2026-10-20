package workflow

import (
	"errors"
	"testing"
)

func publishedActive() *Workflow {
	one := 1
	return &Workflow{ID: "wf-1", WorkspaceID: "ws-1", Status: WorkflowStatusActive, Type: WorkflowTypeMessages, PublishedVersion: &one}
}

func TestStartsConversationsInRefusesWhatCannotRunForThisConversation(t *testing.T) {
	cases := []struct {
		name      string
		workflow  func() *Workflow
		workspace string
		want      error
	}{
		{"an active messages workflow of the workspace starts", publishedActive, "ws-1", nil},
		{"a missing workflow is not found", func() *Workflow { return nil }, "ws-1", ErrWorkflowNotFound},
		{"another workspace's workflow is not found", publishedActive, "ws-2", ErrWorkflowNotFound},
		{"an empty workspace is not found", publishedActive, "", ErrWorkflowNotFound},
		{"a draft does not start", func() *Workflow { w := publishedActive(); w.Status = WorkflowStatusDraft; return w }, "ws-1", ErrWorkflowNotActive},
		{"a paused workflow does not start", func() *Workflow { w := publishedActive(); w.Status = WorkflowStatusPaused; return w }, "ws-1", ErrWorkflowNotActive},
		{"a workflow never published does not start", func() *Workflow { w := publishedActive(); w.PublishedVersion = nil; return w }, "ws-1", ErrWorkflowNotPublished},
		{"a voice workflow cannot take a conversation", func() *Workflow { w := publishedActive(); w.Type = WorkflowTypeVoice; return w }, "ws-1", ErrInvalidWorkflowType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.workflow().StartsConversationsIn(tc.workspace)
			if tc.want == nil && err != nil {
				t.Fatalf("expected no refusal, got %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestCallableByAgentInIsTheOneRuleTheToolAndTheStarterShare(t *testing.T) {
	withTrigger := func(inputs []interface{}) func() *Workflow {
		return func() *Workflow {
			w := publishedActive()
			w.Graph = Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerAgentCall, Config: map[string]interface{}{AgentCallInputsKey: inputs}}}}
			return w
		}
	}
	valid := []interface{}{map[string]interface{}{"name": "cpf", "type": "string", "required": true}}
	cases := []struct {
		name     string
		workflow func() *Workflow
		want     error
		inputs   int
	}{
		{"published, active, with the trigger", withTrigger(valid), nil, 1},
		{"paused is not advertised", func() *Workflow { w := withTrigger(valid)(); w.Status = WorkflowStatusPaused; return w }, ErrWorkflowNotActive, 0},
		{"never published is not advertised", func() *Workflow { w := withTrigger(valid)(); w.PublishedVersion = nil; return w }, ErrWorkflowNotPublished, 0},
		{"without the agent trigger", publishedActive, ErrTriggerNotInWorkflow, 0},
		{"with broken inputs", withTrigger([]interface{}{"cpf"}), ErrAgentCallInputsInvalid, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inputs, err := tc.workflow().CallableByAgentIn("ws-1")
			if tc.want == nil && err != nil {
				t.Fatalf("expected callable, got %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if len(inputs) != tc.inputs {
				t.Fatalf("inputs = %+v", inputs)
			}
		})
	}
	if _, err := withTrigger(valid)().CallableByAgentIn("ws-2"); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("another workspace: %v", err)
	}
}
