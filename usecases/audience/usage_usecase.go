package audience_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	ca "vozko/domain/audience"
)

// What the workspace has analysed against its ceiling, for the dashboard.
//
// This exists because the ceiling was invisible. It could be edited on one
// channel's account page and nowhere else, the number consumed against it was
// never read back at all, and the first sign of hitting it was analysis quietly
// stopping. A screen that reports coverage has to be able to say "and this is
// why coverage is low", which takes three numbers: the ceiling, the spend, and
// the queue standing behind them.

type usageUseCase struct {
	limiter ca.UsageLimiter
	// workspaceLimits is where the ceiling an operator sets actually lives.
	workspaceLimits ca.WorkspaceSettingsStore
	// settings carries the per-account ceilings this control replaced. Still
	// read, never written: a workspace configured before the workspace-level
	// control existed keeps running under the number it already had.
	settings ca.SettingsRepository
	// backlog is what is queued behind the ceiling. Optional, and read
	// defensively: a screen that cannot count the queue is still worth showing.
	backlog ca.BacklogReader
	clock   ca.Clock
}

// NewUsageUseCase reads and writes the rolling budget for one workspace.
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

// limitFor is the ceiling in force, resolved the same way the engine resolves
// it: the workspace's own number, else the highest one any of its accounts
// carries, else the product default.
//
// The account step exists only for workspaces that predate the workspace-level
// control. Taking the HIGHEST of several is deliberately the generous reading:
// it is the one a pass can actually run under, and understating it here would
// make coverage look worse than it is. Setting a workspace ceiling replaces the
// guess with an exact number, which is most of the point of the control.
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

// waitingFor is the queue standing behind the ceiling, or zero if it cannot be
// counted.
//
// Degraded rather than fatal on purpose. The backlog is the part of this screen
// that explains the rest, but the ceiling and the spend are useful without it,
// and failing the whole request over a count would replace a slightly less
// informative panel with no panel at all.
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

// SetLimit changes the ceiling for the whole workspace.
//
// One write to the workspace's own row, which is the only shape that matches
// what the number means: the budget is claimed per workspace, so a ceiling
// stored per (source, account) had no single value and no home at all for a
// workspace that analyses only conversations and has no channel account.
//
// Notably it does NOT write the per-account rows. Doing so would mean creating
// settings rows for every conversation channel, and for a conversation an
// absent row is not a neutral default: the resolver reads "never configured" as
// enabled, so materialising one would turn conversation analysis OFF across the
// workspace as a side effect of setting a budget. It would also add each
// channel to the dashboard's account picker as if it were a configured account.
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
	// Read first, change one field, write back. The record holds more than the
	// ceiling, so building a fresh one here would blank whatever else is on it.
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
