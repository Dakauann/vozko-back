package media_usecase

import (
	"time"

	"vozko/domain/media"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// holdMusicQuotaGate enforces the custom hold music cap: the workspace's ACTIVE
// plan grants PlanDefinition.MaxHoldMusicTracks, clamped to the product hard cap
// of 10 (media.HoldMusicHardCap). Plan driven only, deliberately without addon
// entitlement threading (mirrors the branch gate shape minus the resolver).
type holdMusicQuotaGate struct {
	subs  workspace_plan.CurrentSubscriptionReader
	plans workspace_plan.PlanReader
	repo  media.MediaRepository
	now   func() time.Time
}

func NewHoldMusicQuotaGate(
	subs workspace_plan.CurrentSubscriptionReader,
	plans workspace_plan.PlanReader,
	repo media.MediaRepository,
) media.HoldMusicQuotaGate {
	return &holdMusicQuotaGate{subs: subs, plans: plans, repo: repo, now: time.Now}
}

func (g *holdMusicQuotaGate) CanUploadHoldMusic(workspaceID string) error {
	sub, err := g.subs.GetCurrentByWorkspaceID(workspaceID, g.now())
	if err != nil {
		return err
	}
	if sub == nil || sub.Status != workspace_plan.SubscriptionStatusActive {
		return media.ErrHoldMusicNotIncluded
	}
	plan, err := g.plans.GetByID(sub.PlanDefinitionID)
	if err != nil {
		return err
	}
	if plan == nil {
		return media.ErrHoldMusicNotIncluded
	}

	limit := plan.MaxHoldMusicTracks
	if limit <= 0 {
		return media.ErrHoldMusicNotIncluded
	}
	if limit > media.HoldMusicHardCap {
		limit = media.HoldMusicHardCap
	}

	count, err := g.repo.CountByWorkspaceIDAndType(workspaceID, media.MediaTypeHoldMusic)
	if err != nil {
		return err
	}
	if count >= int64(limit) {
		return media.ErrHoldMusicQuotaReached
	}
	return nil
}
