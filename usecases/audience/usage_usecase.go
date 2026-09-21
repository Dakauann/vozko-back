package audience_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	ca "vozko/domain/audience"
)

type usageUseCase struct {
	limiter         ca.UsageLimiter
	workspaceLimits ca.WorkspaceSettingsStore
	settings        ca.SettingsRepository
	backlog         ca.BacklogReader
	clock           ca.Clock
}

func NewUsageUseCase(limiter ca.UsageLimiter, workspaceLimits ca.WorkspaceSettingsStore, settings ca.SettingsRepository, backlog ca.BacklogReader, clock ca.Clock) ca.UsageUseCase {
	if clock == nil {
		clock = systemClock{}
	}
	return &usageUseCase{
		limiter:         limiter,
		workspaceLimits: workspaceLimits,
		settings:        settings,
		backlog:         backlog,
		clock:           clock,
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (uc *usageUseCase) Execute(ctx context.Context, workspaceID string) (ca.Usage, error) {
	if workspaceID == "" {
		return ca.Usage{}, ca.ErrWorkspaceRequired
	}
	usage, err := uc.limiter.Read(ctx, workspaceID, uc.limitFor(ctx, workspaceID), uc.clock.Now())
	if err != nil {
		return ca.Usage{}, err
	}
	usage.Waiting = uc.waitingFor(ctx, workspaceID)
	return usage, nil
}

func (uc *usageUseCase) limitFor(ctx context.Context, workspaceID string) int {
	workspaceCap := 0
	if uc.workspaceLimits != nil {
		if row, err := uc.workspaceLimits.Get(ctx, workspaceID); err == nil {
			workspaceCap = row.DailyCap
		} else {
			log.Printf("[comment-analysis] workspace ceiling for %s unavailable: %v", workspaceID, err)
		}
	}
	return ca.ResolveDailyCap(workspaceCap, uc.highestAccountCap(ctx, workspaceID))
}

func (uc *usageUseCase) highestAccountCap(ctx context.Context, workspaceID string) int {
	if uc.settings == nil {
		return 0
	}
	rows, err := uc.settings.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return 0
	}
	highest := 0
	for _, s := range rows {
		if s != nil && s.DailyCap > highest {
			highest = s.DailyCap
		}
	}
	return highest
}

func (uc *usageUseCase) waitingFor(ctx context.Context, workspaceID string) int {
	if uc.backlog == nil {
		return 0
	}
	n, err := uc.backlog.CountWaiting(ctx, workspaceID)
	if err != nil {
		log.Printf("[comment-analysis] backlog for workspace %s unavailable: %v", workspaceID, err)
		return 0
	}
	return n
}

func (uc *usageUseCase) SetLimit(ctx context.Context, workspaceID string, limit int) (ca.Usage, error) {
	if workspaceID == "" {
		return ca.Usage{}, ca.ErrWorkspaceRequired
	}
	if limit <= 0 {
		return ca.Usage{}, fmt.Errorf("%w: the analysis limit must be positive", ca.ErrInvalidFilter)
	}
	if uc.workspaceLimits == nil {
		return ca.Usage{}, fmt.Errorf("%w: the analysis limit is not writable here", ca.ErrInvalidFilter)
	}
	current, err := uc.workspaceLimits.Get(ctx, workspaceID)
	if err != nil {
		return ca.Usage{}, err
	}
	current.DailyCap = limit
	if err := uc.workspaceLimits.Save(ctx, workspaceID, current); err != nil {
		return ca.Usage{}, err
	}
	return uc.Execute(ctx, workspaceID)
}
