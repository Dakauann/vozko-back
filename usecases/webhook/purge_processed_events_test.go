package webhook_usecase

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingPurge struct {
	cutoff time.Time
	err    error
}

func (r *recordingPurge) Claim(context.Context, string, string, string) (bool, error) { return true, nil }

func (r *recordingPurge) PurgeOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	r.cutoff = cutoff
	return 3, r.err
}

func TestPurgeUsesTheRetentionWindow(t *testing.T) {
	repo := &recordingPurge{}
	before := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if err := NewPurgeProcessedEventsUseCase(repo, 30*24*time.Hour).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.cutoff.Before(before.Add(-time.Minute)) || repo.cutoff.After(before.Add(time.Minute)) {
		t.Fatalf("cutoff = %v, want about %v", repo.cutoff, before)
	}
}

func TestPurgeSurfacesErrors(t *testing.T) {
	repo := &recordingPurge{err: errors.New("db down")}
	if err := NewPurgeProcessedEventsUseCase(repo, time.Hour).Execute(context.Background()); err == nil {
		t.Fatal("error swallowed")
	}
}
