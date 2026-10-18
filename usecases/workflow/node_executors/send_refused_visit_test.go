package node_executors

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

func atStep(run *workflow.WorkflowRun, durableStep, retry int) {
	run.State.Set(workflow.StateKeyDurableSteps, durableStep)
	run.RetryCount = retry
}

func TestALoopBackToARefusedStepWritesALinePerSendButEngineRetriesDoNot(t *testing.T) {
	lines := &recordedLines{}
	sender := guardedSender(optedOutLead(), &countingWhatsAppClient{}, &recordingHistory{})
	sender.deps.Events = lines
	run := refusedRunAt("send-1")
	send := func() {
		t.Helper()
		if _, _, err := sender.SendText(context.Background(), run, "Promoção!", conversation.MessageTypeAIResponse); !errors.Is(err, wo.ErrLeadOptedOut) {
			t.Fatalf("err = %v", err)
		}
	}

	atStep(run, 3, 0)
	send()
	atStep(run, 4, 1)
	send()
	atStep(run, 5, 2)
	send()
	if len(lines.events) != 1 {
		t.Fatalf("engine retries of one send wrote %d lines", len(lines.events))
	}

	atStep(run, 9, 0)
	send()
	if len(lines.events) != 2 {
		t.Fatalf("the loop's second visit to the step wrote %d lines, want 2", len(lines.events))
	}
}
