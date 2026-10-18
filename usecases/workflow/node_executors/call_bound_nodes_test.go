package node_executors

import (
	"context"
	"testing"
	"time"

	"vozko/domain/workflow"
	email_usecase "vozko/usecases/email"
)

func hangUpAfter(delay time.Duration) *fakeVoiceCall {
	call := &fakeVoiceCall{ended: make(chan struct{})}
	time.AfterFunc(delay, func() { close(call.ended) })
	return call
}

func TestCodeNode_AHangUpStopsTheScript(t *testing.T) {
	state := workflow.NewRunState()
	ctx := &workflow.NodeContext{
		Run:     &workflow.WorkflowRun{ID: "run-1"},
		Node:    &workflow.Node{ID: "c1", Config: map[string]interface{}{"code": "while (true) {}", "timeout_ms": float64(5000)}},
		State:   &state,
		Runtime: hangUpAfter(50 * time.Millisecond),
	}

	started := time.Now()
	if _, err := NewCodeExecutor().Execute(ctx); err == nil {
		t.Fatal("an endless script cut short by a hang-up must report an error")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("the script kept running %s after the caller hung up", elapsed)
	}
}

type waitingEmailSender struct {
	stoppedByHangUp bool
}

func (s *waitingEmailSender) Send(ctx context.Context, _ email_usecase.SMTPConfig, _ email_usecase.EmailMessage) (*email_usecase.SendResult, error) {
	select {
	case <-ctx.Done():
		s.stoppedByHangUp = true
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return &email_usecase.SendResult{}, nil
	}
}

func TestEmailNode_AHangUpStopsTheSend(t *testing.T) {
	sender := &waitingEmailSender{}
	ctx := newSendEmailNodeCtx(validSendEmailNodeConfig())
	ctx.Runtime = hangUpAfter(50 * time.Millisecond)

	res, err := NewSendEmailExecutor(sender).Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !sender.stoppedByHangUp {
		t.Fatal("the SMTP send must stop when the caller hangs up")
	}
	if res.Output["sent"] == true {
		t.Fatal("an interrupted send cannot be reported as sent")
	}
}
