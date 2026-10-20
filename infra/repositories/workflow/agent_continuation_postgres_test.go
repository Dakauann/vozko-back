package workflow_repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"
)

func TestAReleasedContinuationReturnsToTheSweepUntilTheBoundOnPostgres(t *testing.T) {
	db := postgresSchemaTx(t, &schema.WorkflowRunSchema{})
	finishedAt := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Microsecond)
	runID := uuid.NewString()
	run := schema.WorkflowRunSchema{ID: runID, WorkflowID: uuid.NewString(), WorkspaceID: uuid.NewString(), EntryID: uuid.NewString(), EntryType: "instagram", Status: "completed", State: "{}", StartedAt: finishedAt, StartedBy: workflow.StartedByAgent("a-1")}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE workflow_runs SET updated_at = ? WHERE id = ?", finishedAt, runID).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewAgentCallRunsRepository(db)
	backlog := func() bool {
		runs, err := repo.FinishedWithoutContinuation(finishedAt.Add(-time.Minute), finishedAt.Add(time.Minute), 10)
		if err != nil {
			t.Fatal(err)
		}
		return len(runs) == 1 && runs[0].ID == runID
	}

	if released, err := repo.ReleaseAgentContinuation(runID); err != nil || released {
		t.Fatalf("an unclaimed run has nothing to release: %v, %v", released, err)
	}
	for attempt := 1; attempt <= workflow.MaxAgentContinuationAttempts; attempt++ {
		if claimed, err := repo.ClaimAgentContinuation(runID); err != nil || !claimed {
			t.Fatalf("attempt %d: claim = %v, %v", attempt, claimed, err)
		}
		if backlog() {
			t.Fatalf("attempt %d: a claimed run must leave the backlog", attempt)
		}
		released, err := repo.ReleaseAgentContinuation(runID)
		if err != nil {
			t.Fatal(err)
		}
		if want := attempt < workflow.MaxAgentContinuationAttempts; released != want {
			t.Fatalf("attempt %d: released = %v, want %v", attempt, released, want)
		}
		if released && !backlog() {
			t.Fatalf("attempt %d: a released run must be back in the sweep's backlog", attempt)
		}
	}
	if claimed, err := repo.ClaimAgentContinuation(runID); err != nil || claimed {
		t.Fatalf("after the bound the claim stays: %v, %v", claimed, err)
	}
	var updatedAt time.Time
	if err := db.Raw("SELECT updated_at FROM workflow_runs WHERE id = ?", runID).Scan(&updatedAt).Error; err != nil || !updatedAt.Equal(finishedAt) {
		t.Fatalf("updated_at = %v, %v; a release must not move the run into a later sweep window", updatedAt, err)
	}
}

func TestReleasingAContinuationBindsTheRunAndTheBound(t *testing.T) {
	if got := strings.Count(releaseAgentContinuationSQL, "?"); got != 2 {
		t.Fatalf("placeholders = %d, want 2", got)
	}
	if !strings.Contains(claimAgentContinuationSQL, "agent_continuation_attempts = agent_continuation_attempts + 1") {
		t.Fatalf("every claim counts as an attempt:\n%s", claimAgentContinuationSQL)
	}
}
