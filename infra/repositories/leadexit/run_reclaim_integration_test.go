package leadexit_repository

import (
	"context"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/leadexit"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestARunLeftRunningPastTheStepTimeoutIsClaimedAgainOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "leadexitreclaim", &schema.LeadExitRun{})
	store := NewRunStore(db)
	ctx := context.Background()
	run := leadexit.Run{
		WorkspaceID: workspaceA, InboundMessageID: "wamid.stuck", EntryID: "entry-1", EntryType: "whatsapp", LeadID: "lead-1",
		Trigger: leadexit.TriggerTemplateWord, Kind: buttonaction.KindOptOut,
	}
	if claim, err := store.Claim(ctx, run); err != nil || !claim.Claimed {
		t.Fatalf("first claim = %+v, %v", claim, err)
	}
	backdate := func(seconds float64) {
		t.Helper()
		if err := db.Exec(`UPDATE lead_exit_runs SET claimed_at = NOW() - (? * INTERVAL '1 second') WHERE inbound_message_id = ?`, seconds, run.InboundMessageID).Error; err != nil {
			t.Fatal(err)
		}
	}

	backdate(leadexit.RunTimeout.Seconds() - 5)
	if claim, err := store.Claim(ctx, run); err != nil || claim.Claimed || claim.Previous.Outcome != leadexit.OutcomeRunning {
		t.Fatalf("a run still inside its timeout = %+v, %v", claim, err)
	}

	backdate(leadexit.RunTimeout.Seconds() + 1)
	if claim, err := store.Claim(ctx, run); err != nil || !claim.Claimed {
		t.Fatalf("a run past its timeout = %+v, %v", claim, err)
	}
}
