package workflow

import (
	"errors"
	"testing"
)

func TestStartsConversationsInRefusesWhatCannotRunForThisConversation(t *testing.T) {
	active := func() *Workflow {
		return &Workflow{ID: "wf-1", WorkspaceID: "ws-1", Status: WorkflowStatusActive, Type: WorkflowTypeMessages}
	}
	cases := []struct {
		name      string
		workflow  func() *Workflow
		workspace string
		want      error
	}{
		{"an active messages workflow of the workspace starts", active, "ws-1", nil},
		{"a missing workflow is not found", func() *Workflow { return nil }, "ws-1", ErrWorkflowNotFound},
		{"another workspace's workflow is not found", active, "ws-2", ErrWorkflowNotFound},
		{"an empty workspace is not found", active, "", ErrWorkflowNotFound},
		{"a draft does not start", func() *Workflow { w := active(); w.Status = WorkflowStatusDraft; return w }, "ws-1", ErrWorkflowNotActive},
		{"a paused workflow does not start", func() *Workflow { w := active(); w.Status = WorkflowStatusPaused; return w }, "ws-1", ErrWorkflowNotActive},
		{"a voice workflow cannot take a conversation", func() *Workflow { w := active(); w.Type = WorkflowTypeVoice; return w }, "ws-1", ErrInvalidWorkflowType},
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
