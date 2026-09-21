package audience_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	ca "vozko/domain/audience"
	"vozko/domain/cache"
	"vozko/domain/shared"
)

const (
	hourlyCallBudget = 100
	backfillCallKey  = "comment_analysis:backfill:calls:"

	pagesPerTick = 40

	containerPageSize = 100
)

var (
	ErrBackfillAlreadyActive = errors.New("comment analysis: a backfill is already running for this target")
	ErrBackfillEstimateStale = errors.New("comment analysis: the confirmed estimate does not match; refresh and confirm again")
	ErrBackfillNotCancelable = errors.New("comment analysis: this backfill can no longer be cancelled")
)

type BackfillDeps struct {
	Backfills ca.BackfillRepository
	Settings  ca.SettingsRepository
	Ingestor  ca.Ingestor
	Adapters  map[ca.Source]ca.SourceAdapter
	Verifiers map[ca.Source]AccountVerifier
	State     cache.SharedState
	Clock     ca.Clock
}

type backfillUseCases struct{ BackfillDeps }

func NewBackfillUseCases(deps BackfillDeps) (ca.EstimateBackfillUseCase, ca.StartBackfillUseCase, ca.GetBackfillUseCase, ca.CancelBackfillUseCase) {
	if deps.Clock == nil {
		deps.Clock = shared.SystemClock{}
	}
	uc := &backfillUseCases{BackfillDeps: deps}
	return estimateBackfill{uc}, startBackfill{uc}, getBackfill{uc}, cancelBackfill{uc}
}

func (uc *backfillUseCases) verify(ctx context.Context, workspaceID string, source ca.Source, accountID string) error {
	if !source.Valid() || strings.TrimSpace(accountID) == "" {
		return ca.ErrContainerInvalid
	}
	v, ok := uc.Verifiers[source]
	if !ok {
		return ca.ErrContainerInvalid
	}
	owns, err := v.AccountBelongsTo(ctx, workspaceID, accountID)
	if err != nil {
		return err
	}
	if !owns {
		return ca.ErrNotFound
	}
	return nil
}

func (uc *backfillUseCases) estimate(ctx context.Context, workspaceID string, source ca.Source, accountID, containerID string) (*ca.BackfillEstimate, error) {
	adapter, ok := uc.Adapters[source]
	if !ok {
		return nil, ca.ErrContainerInvalid
	}
	est := &ca.BackfillEstimate{}
	offset := 0
	for {
		page, err := adapter.ListContainers(ctx, accountID, containerPageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, c := range page {
			if containerID != "" && c.Ref.ContainerID != containerID {
				continue
			}
			est.Containers++
			est.EstimatedComments += c.CommentsCount
		}
		if len(page) < containerPageSize {
			break
		}
		offset += containerPageSize
	}
	return est, nil
}

type estimateBackfill struct{ *backfillUseCases }

func (e estimateBackfill) Execute(ctx context.Context, workspaceID string, source ca.Source, accountID, containerID string) (*ca.BackfillEstimate, error) {
	workspaceID, accountID, containerID = strings.TrimSpace(workspaceID), strings.TrimSpace(accountID), strings.TrimSpace(containerID)
	if err := e.verify(ctx, workspaceID, source, accountID); err != nil {
		return nil, err
	}
	return e.estimate(ctx, workspaceID, source, accountID, containerID)
}

type startBackfill struct{ *backfillUseCases }

func (s startBackfill) Execute(ctx context.Context, in ca.StartBackfillInput) (*ca.Backfill, error) {
	in.WorkspaceID, in.AccountID, in.ContainerID = strings.TrimSpace(in.WorkspaceID), strings.TrimSpace(in.AccountID), strings.TrimSpace(in.ContainerID)
	if err := s.verify(ctx, in.WorkspaceID, in.Source, in.AccountID); err != nil {
		return nil, err
	}
	settings, err := s.Settings.Find(ctx, in.Source, in.AccountID)
	if err != nil || !settings.Enabled {
		return nil, fmt.Errorf("%w: analysis is not enabled for this account", ca.ErrInvalidFilter)
	}
	if existing, err := s.Backfills.FindActive(ctx, in.Source, in.AccountID, in.ContainerID); err == nil && existing != nil {
		return nil, ErrBackfillAlreadyActive
	} else if err != nil && !errors.Is(err, ca.ErrNotFound) {
		return nil, err
	}
	est, err := s.estimate(ctx, in.WorkspaceID, in.Source, in.AccountID, in.ContainerID)
	if err != nil {
		return nil, err
	}
	if est.EstimatedComments != in.ConfirmedEstimate {
		return nil, ErrBackfillEstimateStale
	}
	now := s.Clock.Now()
	b := &ca.Backfill{
		ID:                uuid.NewString(),
		WorkspaceID:       in.WorkspaceID,
		Source:            in.Source,
		AccountID:         in.AccountID,
		ContainerID:       in.ContainerID,
		Status:            ca.BackfillPending,
		EstimatedComments: est.EstimatedComments,
		RequestedByUserID: strings.TrimSpace(in.RequestedByUserID),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.Backfills.Create(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

type getBackfill struct{ *backfillUseCases }

func (g getBackfill) Execute(ctx context.Context, workspaceID, id string) (*ca.Backfill, error) {
	return g.Backfills.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
}

type cancelBackfill struct{ *backfillUseCases }

func (c cancelBackfill) Execute(ctx context.Context, workspaceID, id string) (*ca.Backfill, error) {
	b, err := c.Backfills.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := b.Finish(ca.BackfillCanceled, "", c.Clock.Now()); err != nil {
		return nil, ErrBackfillNotCancelable
	}
	if err := c.Backfills.Save(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

type BackfillJob struct{ *backfillUseCases }

func NewBackfillJob(deps BackfillDeps) *BackfillJob {
	if deps.Clock == nil {
		deps.Clock = shared.SystemClock{}
	}
	return &BackfillJob{&backfillUseCases{BackfillDeps: deps}}
}

func (j *BackfillJob) Execute(ctx context.Context) error {
	now := j.Clock.Now()
	b, err := j.Backfills.ClaimNextPending(ctx, now)
	if err != nil || b == nil {
		return err
	}
	adapter, ok := j.Adapters[b.Source]
	if !ok {
		_ = b.Finish(ca.BackfillFailed, "no adapter for source", now)
		return j.Backfills.Save(ctx, b)
	}

	containers, err := j.targets(ctx, adapter, b)
	if err != nil {
		_ = b.Finish(ca.BackfillFailed, err.Error(), now)
		_ = j.Backfills.Save(ctx, b)
		return err
	}

	containerIdx, pageCursor := decodeCursor(b.Cursor)
	pages := 0
	for containerIdx < len(containers) && pages < pagesPerTick {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		allowed, err := j.allowCall(b.AccountID, now)
		if err != nil || !allowed {
			b.Cursor = encodeCursor(containerIdx, pageCursor)
			_ = b.Pause(now)
			return j.Backfills.Save(ctx, b)
		}
		ref := containers[containerIdx].Ref
		items, next, err := adapter.FetchCommentsPage(ctx, ref, pageCursor)
		if err != nil {
			b.Cursor = encodeCursor(containerIdx, pageCursor)
			_ = b.Finish(ca.BackfillFailed, err.Error(), now)
			_ = j.Backfills.Save(ctx, b)
			return err
		}
		enqueued := 0
		for _, it := range items {
			if err := j.Ingestor.Enqueue(ctx, it); err != nil {
				log.Printf("[comment-analysis-backfill] enqueue %s: %v", it.SubjectID, err)
				continue
			}
			enqueued++
		}
		pages++
		if next == "" {
			containerIdx++
			pageCursor = ""
		} else {
			pageCursor = next
		}
		b.Advance(encodeCursor(containerIdx, pageCursor), len(items), enqueued, now)
		if err := j.Backfills.Save(ctx, b); err != nil {
			return err
		}
	}

	if containerIdx >= len(containers) {
		_ = b.Finish(ca.BackfillDone, "", now)
	} else {
		_ = b.Pause(now)
	}
	return j.Backfills.Save(ctx, b)
}

func (j *BackfillJob) targets(ctx context.Context, adapter ca.SourceAdapter, b *ca.Backfill) ([]ca.ContainerSummary, error) {
	var out []ca.ContainerSummary
	offset := 0
	for {
		page, err := adapter.ListContainers(ctx, b.AccountID, containerPageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, c := range page {
			if b.ContainerID == "" || c.Ref.ContainerID == b.ContainerID {
				out = append(out, c)
			}
		}
		if len(page) < containerPageSize {
			return out, nil
		}
		offset += containerPageSize
	}
}

func (j *BackfillJob) allowCall(accountID string, now time.Time) (bool, error) {
	key := backfillCallKey + accountID + ":" + now.UTC().Format("2006-01-02-15")
	ok, err := j.State.TryIncrBy(key, 1, hourlyCallBudget)
	if err != nil {
		return false, err
	}
	_, _ = j.State.Expire(key, 2*time.Hour)
	return ok, nil
}

func encodeCursor(idx int, page string) string {
	return fmt.Sprintf("%d|%s", idx, page)
}

func decodeCursor(s string) (int, string) {
	if s == "" {
		return 0, ""
	}
	var idx int
	var page string
	if i := strings.IndexByte(s, '|'); i >= 0 {
		fmt.Sscanf(s[:i], "%d", &idx)
		page = s[i+1:]
	}
	return idx, page
}
