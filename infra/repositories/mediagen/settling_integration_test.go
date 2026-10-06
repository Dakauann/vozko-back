package mediagen_repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/mediagen"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func runningJob(t *testing.T, repo mediagen.Repository, kind mediagen.Kind, fingerprint string) *mediagen.Job {
	t.Helper()
	ctx := context.Background()
	job := &mediagen.Job{WorkspaceID: uuid.NewString(), RequestedBy: uuid.NewString(), Kind: kind, Prompt: "samba", Fingerprint: fingerprint, Status: mediagen.StatusQueued}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := repo.Claim(ctx, job.ID)
	if err != nil || !ok {
		t.Fatalf("claim %v %v", ok, err)
	}
	return claimed
}

func TestASettlingJobIsSettledOnceIntoItsOutcome(t *testing.T) {
	db := repotest.IsolatedDB(t, "mediagen_settling", &schema.MediaGenerationJob{})
	repo := NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	delivered := runningJob(t, repo, mediagen.KindMusic, "fp-delivered")
	result := mediagen.Result{MediaID: "m-1", MediaURL: "https://cdn/m-1.m4a", Model: "google/lyria-3-clip-preview"}
	if err := repo.MarkSettling(ctx, delivered.ID, mediagen.Settlement{GenerationID: "gen-1", Result: &result}, now); err != nil {
		t.Fatal(err)
	}
	failed := runningJob(t, repo, mediagen.KindVoice, "fp-failed")
	if err := repo.MarkSettling(ctx, failed.ID, mediagen.Settlement{GenerationID: "gen-2", Failure: mediagen.FailureGeneration}, now); err != nil {
		t.Fatal(err)
	}

	again, err := repo.FindActive(ctx, delivered.WorkspaceID, delivered.RequestedBy, "fp-delivered", now.Add(-time.Hour))
	if err != nil || again.ID != delivered.ID || again.Status != mediagen.StatusSettling || again.GenerationID != "gen-1" {
		t.Fatalf("a settling job must count as active: %+v %v", again, err)
	}
	pending, err := repo.ListSettling(ctx, now.Add(-time.Hour), 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending %+v err %v", pending, err)
	}

	var wins int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, won, err := repo.Settle(ctx, delivered.ID, now)
			if err != nil {
				t.Error(err)
			}
			if won {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("settled %d times, a cost must be billed once", wins)
	}
	done, _ := repo.Get(ctx, delivered.WorkspaceID, delivered.ID)
	if done.Status != mediagen.StatusDone || done.MediaID != "m-1" {
		t.Fatalf("delivered job %+v", done)
	}
	settled, won, err := repo.Settle(ctx, failed.ID, now)
	if err != nil || !won || settled.Status != mediagen.StatusFailed || settled.FailureCode != mediagen.FailureGeneration || settled.MediaID != "" {
		t.Fatalf("failed job %+v won %v err %v", settled, won, err)
	}
}

func TestOnlyOldSettlingJobsExpire(t *testing.T) {
	db := repotest.IsolatedDB(t, "mediagen_expire", &schema.MediaGenerationJob{})
	repo := NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	old := runningJob(t, repo, mediagen.KindMusic, "fp-old")
	if err := repo.MarkSettling(ctx, old.ID, mediagen.Settlement{GenerationID: "gen-old", Failure: mediagen.FailureGeneration}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schema.MediaGenerationJob{}).Where("id = ?", old.ID).Update("created_at", now.Add(-8*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	fresh := runningJob(t, repo, mediagen.KindMusic, "fp-fresh")
	if err := repo.MarkSettling(ctx, fresh.ID, mediagen.Settlement{GenerationID: "gen-fresh", Failure: mediagen.FailureGeneration}, now); err != nil {
		t.Fatal(err)
	}
	ids, err := repo.ExpireSettling(ctx, mediagen.SettleSince(now), 10)
	if err != nil || len(ids) != 1 || ids[0] != old.ID {
		t.Fatalf("expired %v err %v", ids, err)
	}
	expired, _ := repo.Get(ctx, old.WorkspaceID, old.ID)
	still, _ := repo.Get(ctx, fresh.WorkspaceID, fresh.ID)
	if expired.Status != mediagen.StatusFailed || expired.FailureCode != mediagen.FailureCostUnreported || still.Status != mediagen.StatusSettling {
		t.Fatalf("expired %+v still %+v", expired, still)
	}
	stale, err := repo.FailStale(ctx, now.Add(time.Hour), 10)
	if err != nil || len(stale) != 0 {
		t.Fatalf("the stale reaper must never touch a settling job: %v %v", stale, err)
	}
}

func TestASettlementWithoutAGenerationIsRefused(t *testing.T) {
	db := repotest.IsolatedDB(t, "mediagen_refuse", &schema.MediaGenerationJob{})
	repo := NewJobRepository(db)
	job := runningJob(t, repo, mediagen.KindMusic, "fp")
	if err := repo.MarkSettling(context.Background(), job.ID, mediagen.Settlement{Failure: mediagen.FailureGeneration}, time.Now()); err == nil {
		t.Fatal("a settlement without a generation id can never be billed")
	}
}
