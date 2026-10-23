package workflow_usecase

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/conversation"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

func TestAPermanentSendRefusalFailsTheRunAtOnceInsteadOfRetrying(t *testing.T) {
	cases := []struct {
		name string
		err  error
		kind workflow.ErrorKind
	}{
		{"a closed window", fmt.Errorf("%w (no customer message in the last 24 h). %s", conversation.ErrWindowClosed, workflow.WindowClosedFix), workflow.ErrorKindWindowClosed},
		{"an opted out contact", fmt.Errorf("workflow sender: lead l-1 (lead_opted_out): %w", wo.ErrLeadOptedOut), workflow.ErrorKindInternal},
		{"a blocked contact", fmt.Errorf("workflow sender: lead l-1 (lead_blocked): %w", wo.ErrLeadBlocked), workflow.ErrorKindInternal},
		{"an invalid number", fmt.Errorf("workflow sender: lead l-1 (invalid_phone): %w", wo.ErrInvalidPhone), workflow.ErrorKindInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTraceHarness()
			h.register(workflow.NodeTypeActionSendText, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
				return nil, tc.err
			})
			run := h.start(linearWorkflow(workflow.Node{ID: "ola", Type: workflow.NodeTypeActionSendText}))
			if run.Status != workflow.RunStatusError || run.RetryCount != 0 || run.WaitReason == workflow.WaitReasonRetry {
				t.Fatalf("run = %s/%s retry %d, want it failed with no retry", run.Status, run.WaitReason, run.RetryCount)
			}
			step := h.step(t, "ola")
			if step.Status != workflow.LogStatusFailed || step.Attempt != 1 || step.ErrorKind != tc.kind {
				t.Fatalf("step = %+v", step)
			}
		})
	}
}

func TestATransientSendFailureIsStillRetried(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionSendText, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return nil, errors.New("provider unavailable")
	})
	run := h.start(linearWorkflow(workflow.Node{ID: "ola", Type: workflow.NodeTypeActionSendText}))
	if run.Status != workflow.RunStatusWaiting || run.WaitReason != workflow.WaitReasonRetry || run.RetryCount != 1 {
		t.Fatalf("run = %s/%s retry %d, want a retry wait", run.Status, run.WaitReason, run.RetryCount)
	}
}

func TestAFailedCodeNodeFailsTheRunAtOnceBecauseItsSideEffectsCannotBeReplayed(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return nil, workflow.Fail(workflow.ErrorKindTimeout, errors.New("action_code: scriptvm: execution timeout"))
	})
	run := h.start(linearWorkflow(workflow.Node{ID: "matricula", Type: workflow.NodeTypeActionCode}))
	if run.Status != workflow.RunStatusError || run.RetryCount != 0 || run.WaitReason == workflow.WaitReasonRetry {
		t.Fatalf("run = %s/%s retry %d, want it failed with no retry", run.Status, run.WaitReason, run.RetryCount)
	}
	if step := h.step(t, "matricula"); step.Status != workflow.LogStatusFailed || step.Attempt != 1 {
		t.Fatalf("step = %+v", step)
	}
}
