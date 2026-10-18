package leadexit_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/leadexit"
)

func TestAFailedOptOutIsWrittenAsFailedAndRunsAgainOnTheRetry(t *testing.T) {
	f := sairFixture(t)
	f.leads.failSaves = 1
	replier := &fakeReplier{}
	in := tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR")

	if _, err := f.step.Run(context.Background(), in, replier); err == nil {
		t.Fatal("a failed opt-out must fail the inbound so it is tried again")
	}
	if line := f.events.last(t); line["kind"] != "opt_out" || line["outcome"] != "failed" || line["reason"] != "lead_exit_action_failed" {
		t.Fatalf("line = %+v", line)
	}

	result, err := f.step.Run(context.Background(), in, replier)

	if err != nil || !result.OptedOut || f.leads.get(t, "lead-1").OptedOutAt == nil {
		t.Fatalf("retry = %+v, %v", result, err)
	}
	if len(replier.sent) != 1 {
		t.Fatalf("confirmations = %+v", replier.sent)
	}
}

func TestARunLeftRunningIsTakenAgainOnceTheStepTimeoutPassed(t *testing.T) {
	f := sairFixture(t)
	f.runs.failFinish = 1
	replier := &fakeReplier{}
	in := tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR")

	if _, err := f.step.Run(context.Background(), in, replier); err == nil {
		t.Fatal("an unfinished run must fail the inbound")
	}
	if _, err := f.step.Run(context.Background(), in, replier); !errors.Is(err, leadexit.ErrRunInProgress) {
		t.Fatalf("a fresh running run: got %v, want ErrRunInProgress", err)
	}

	f.now = f.now.Add(leadexit.RunTimeout)
	result, err := f.step.Run(context.Background(), in, replier)

	if err != nil || !result.OptedOut {
		t.Fatalf("reclaimed run = %+v, %v", result, err)
	}
	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeUnchanged {
		t.Fatalf("run = %+v", run)
	}
	if len(replier.sent) != 1 {
		t.Fatalf("confirmations = %+v", replier.sent)
	}
}

func TestTheRunIsFinishedEvenWhenTheInboundContextEnds(t *testing.T) {
	f := sairFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	replier := &fakeReplier{during: cancel}

	if _, err := f.step.Run(ctx, tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), replier); err != nil {
		t.Fatalf("Run = %v", err)
	}

	if len(f.runs.finishCtxOK) != 1 || !f.runs.finishCtxOK[0] {
		t.Fatalf("Finish got a cancelled context: %+v", f.runs.finishCtxOK)
	}
	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeChanged {
		t.Fatalf("run = %+v", run)
	}
}
