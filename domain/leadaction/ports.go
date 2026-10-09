package leadaction

import (
	"context"
	"time"
)

type Store interface {
	Create(ctx context.Context, r *Run) error
	FindByKey(ctx context.Context, workspaceID, key string) (*Run, error)
	Get(ctx context.Context, workspaceID, id string) (*Run, error)
	Claim(ctx context.Context, id, token string, now time.Time) (*Run, error)
	Save(ctx context.Context, r *Run, claim string) error
	Claimable(ctx context.Context, now time.Time, limit int) ([]string, error)
	FailStalled(ctx context.Context, now time.Time) (map[Action]int, error)
}

type Changed struct {
	LeadID  string
	Version int64
}

type BatchWrite struct {
	WorkspaceID string
	ActorID     string
	LeadIDs     []string
	Edit        Edit
	At          time.Time
}

type Tally struct {
	Changed   []Changed
	Unchanged int
	Gone      int
}

func (t Tally) Skipped() map[SkipReason]int {
	skipped := map[SkipReason]int{}
	if t.Unchanged > 0 {
		skipped[SkipUnchanged] = t.Unchanged
	}
	if t.Gone > 0 {
		skipped[SkipGone] = t.Gone
	}
	return skipped
}

type BulkWriter interface {
	ApplyBatch(ctx context.Context, w BatchWrite) (Tally, error)
	TallyBatch(ctx context.Context, workspaceID string, ids []string, e Edit) (Tally, error)
	BlockTargets(ctx context.Context, workspaceID string, ids []string, blocked bool) ([]BlockTarget, error)
}

type BlockTarget struct {
	LeadID string
	Number string
}

type BulkNotifier interface {
	LeadsBulkUpdated(workspaceID, runID string)
}
