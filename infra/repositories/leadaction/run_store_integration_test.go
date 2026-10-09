package leadaction_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/leadaction"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestRunsClaimOnceAndResumeAfterADeadWorkerAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDBWith(t, "lead_action_runs", repotest.Options{PrepareStmt: true}, &schema.LeadActionRun{})
	store := NewStore(db)
	ctx := context.Background()
	ws := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	blocked := true
	run, err := leadaction.NewRun(leadaction.RunRequest{ID: uuid.NewString(), WorkspaceID: ws, ActorID: uuid.NewString(), Action: leadaction.ActionBlock,
		Params: leadaction.Params{Blocked: &blocked}, IdempotencyKey: "key-1", RequestFingerprint: "fp", Matched: 3, Selected: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	twin := *run
	twin.ID = uuid.NewString()
	if err := store.Create(ctx, &twin); !errors.Is(err, leadaction.ErrRunExists) {
		t.Fatalf("a second run with the same key = %v", err)
	}

	claimed, err := store.Claim(ctx, run.ID, "worker-1", now)
	if err != nil || claimed.Status != leadaction.StatusRunning || claimed.Attempts != 1 {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	if _, err := store.Claim(ctx, run.ID, "worker-2", now); !errors.Is(err, leadaction.ErrClaimLost) {
		t.Fatalf("a live run was claimed twice: %v", err)
	}
	claimed.Advance(leadaction.BatchOutcome{Cursor: uuid.NewString(), Processed: 2, Changed: 2}, now)
	if err := store.Save(ctx, claimed, "worker-2"); !errors.Is(err, leadaction.ErrClaimLost) {
		t.Fatalf("a save with another claim = %v", err)
	}
	if err := store.Save(ctx, claimed, "worker-1"); err != nil {
		t.Fatal(err)
	}

	later := now.Add(leadaction.StaleAfter + time.Minute)
	ids, err := store.Claimable(ctx, later, 10)
	if err != nil || len(ids) != 1 || ids[0] != run.ID {
		t.Fatalf("claimable = %v, %v", ids, err)
	}
	resumed, err := store.Claim(ctx, run.ID, "worker-3", later)
	if err != nil || resumed.Attempts != 2 || resumed.Result.Processed != 2 || resumed.Cursor != claimed.Cursor {
		t.Fatalf("resumed = %+v, %v", resumed, err)
	}
	if err := db.Exec("UPDATE lead_action_runs SET attempts = ? WHERE id = ?", leadaction.MaxAttempts, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	failed, err := store.FailStalled(ctx, later.Add(leadaction.StaleAfter+time.Minute))
	if err != nil || len(failed) != 1 || failed[leadaction.ActionBlock] != 1 {
		t.Fatalf("stalled = %v, %v", failed, err)
	}
	final, err := store.Get(ctx, ws, run.ID)
	if err != nil || final.Status != leadaction.StatusFailed || final.FailureCode != leadaction.FailureStalled {
		t.Fatalf("final = %+v, %v", final, err)
	}
	if _, err := store.Get(ctx, uuid.NewString(), run.ID); !errors.Is(err, leadaction.ErrRunNotFound) {
		t.Fatalf("another workspace read the run: %v", err)
	}
}
