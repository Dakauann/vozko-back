package leadexit_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/leadexit"
)

func TestARouletteDrawWhoseStepOutFailedIsKeptAndNeverDrawnAgain(t *testing.T) {
	f := tapFixture(t, handOffWithConfirmation())
	f.handOff.drawnButErr = errors.New("automation step-out failed")
	replier := &fakeReplier{}
	in := tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Botão")

	if _, err := f.step.Run(context.Background(), in, replier); err != nil {
		t.Fatalf("a drawn hand off must not be retried: %v", err)
	}
	if _, err := f.step.Run(context.Background(), in, replier); err != nil {
		t.Fatalf("redelivery: %v", err)
	}

	if len(f.handOff.roulette) != 1 {
		t.Fatalf("the ring was drawn %d times", len(f.handOff.roulette))
	}
	run := f.runs.of("wamid.in")
	if run.Outcome != leadexit.OutcomeChanged || run.AssignedUserID != "user-9" || run.Reason != "lead_exit_action_failed" || run.Kind != buttonaction.KindHandOff {
		t.Fatalf("run = %+v", run)
	}
	if line := f.events.last(t); line["outcome"] != "changed" || line["assigned_user_id"] != "user-9" || line["reason"] != "lead_exit_action_failed" {
		t.Fatalf("line = %+v", line)
	}
	if len(replier.sent) != 1 {
		t.Fatalf("confirmations = %+v", replier.sent)
	}
}
