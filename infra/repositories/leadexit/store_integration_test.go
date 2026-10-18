package leadexit_repository

import (
	"context"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/leadexit"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestAnExitRunIsClaimedOncePerInboundMessageOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "leadexitruns", &schema.LeadExitRun{})
	store := NewRunStore(db)
	ctx := context.Background()
	run := leadexit.Run{
		WorkspaceID: workspaceA, InboundMessageID: "wamid.in", EntryID: "entry-1", EntryType: "whatsapp", LeadID: "lead-1",
		TemplateMessageID: "wamid.tmpl", Trigger: leadexit.TriggerButton, Kind: buttonaction.KindOptOut,
	}

	first, err := store.Claim(ctx, run)
	if err != nil || !first.Claimed {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	busy, err := store.Claim(ctx, run)
	if err != nil || busy.Claimed || busy.Previous.Outcome != leadexit.OutcomeRunning {
		t.Fatalf("claim while running = %+v, %v", busy, err)
	}

	run.Outcome, run.Reason = leadexit.OutcomeFailed, "lead_exit_action_failed"
	if err := store.Finish(ctx, run); err != nil {
		t.Fatal(err)
	}
	retry, err := store.Claim(ctx, run)
	if err != nil || !retry.Claimed {
		t.Fatalf("claim after a failure = %+v, %v", retry, err)
	}

	run.Outcome, run.Reason, run.ConsentEventID, run.Confirmation = leadexit.OutcomeChanged, "", "ev-1", leadexit.ConfirmationSent
	if err := store.Finish(ctx, run); err != nil {
		t.Fatal(err)
	}
	done, err := store.Claim(ctx, run)
	if err != nil || done.Claimed {
		t.Fatalf("claim after it ran = %+v, %v", done, err)
	}
	previous := done.Previous
	if previous.Outcome != leadexit.OutcomeChanged || previous.Kind != buttonaction.KindOptOut || previous.Trigger != leadexit.TriggerButton ||
		previous.ConsentEventID != "ev-1" || previous.Confirmation != leadexit.ConfirmationSent || previous.TemplateMessageID != "wamid.tmpl" || !previous.OptedOut() {
		t.Fatalf("previous = %+v", previous)
	}

	other := run
	other.WorkspaceID = "7f1d1c52-2a6c-4b8e-9f43-1c1f5b7d2a02"
	if claim, err := store.Claim(ctx, other); err != nil || !claim.Claimed {
		t.Fatalf("the same message id in another workspace = %+v, %v", claim, err)
	}
}
