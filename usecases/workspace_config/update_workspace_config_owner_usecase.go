package workspace_config_usecase

import (
	"context"
	"fmt"
	"time"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/workspace"
	wsc "vozko/domain/workspace_config"
)

type workspaceOwnerFetcher interface {
	GetWorkspaceByID(id string) (*workspace.Workspace, error)
	ListMembers(workspaceID string) ([]*workspace.Member, error)
}

type updateWorkspaceConfigOwnerUseCase struct {
	repo   wsc.Repository
	wsRepo workspaceOwnerFetcher
	clock  func() time.Time
}

func NewUpdateWorkspaceConfigOwnerUseCase(repo wsc.Repository, wsRepo workspaceOwnerFetcher) wsc.UpdateWorkspaceConfigOwnerUseCase {
	return &updateWorkspaceConfigOwnerUseCase{repo: repo, wsRepo: wsRepo}
}

func (uc *updateWorkspaceConfigOwnerUseCase) SetClock(clock func() time.Time) {
	if uc != nil && clock != nil {
		uc.clock = clock
	}
}

func (uc *updateWorkspaceConfigOwnerUseCase) now() time.Time {
	if uc == nil || uc.clock == nil {
		return time.Now().UTC()
	}
	return uc.clock()
}

func (uc *updateWorkspaceConfigOwnerUseCase) Execute(ctx context.Context, workspaceID, callerID, callerRole string, input wsc.UpdateWorkspaceConfigOwnerInput) (*wsc.WorkspaceConfig, error) {
	if callerRole != "admin" {
		ws, err := uc.wsRepo.GetWorkspaceByID(workspaceID)
		if err != nil || ws == nil {
			return nil, wsc.ErrForbidden
		}
		if ws.OwnerID != callerID {
			return nil, wsc.ErrForbidden
		}
	}

	existing, err := uc.repo.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	if input.SkipAdminAssignment != nil {
		existing.SkipAdminAssignment = *input.SkipAdminAssignment
	}
	if input.AutoCloseEnabled != nil {
		existing.AutoCloseEnabled = *input.AutoCloseEnabled
	}
	if input.AutoCloseIdleAfterHours != nil {
		existing.AutoCloseIdleAfterHours = wsc.ClampAutoCloseIdleHours(*input.AutoCloseIdleAfterHours)
	}
	if input.AutoCloseMaxAgeEnabled != nil {
		existing.AutoCloseMaxAgeEnabled = *input.AutoCloseMaxAgeEnabled
	}
	if input.AutoCloseMaxAgeAfterHours != nil {
		existing.AutoCloseMaxAgeAfterHours = wsc.ClampAutoCloseMaxAgeHours(*input.AutoCloseMaxAgeAfterHours)
	}
	if input.RouletteMode != nil {
		existing.RouletteMode = wsc.NormalizeRouletteMode(*input.RouletteMode)
	}
	if input.RouletteLastSeenWindowHours != nil {
		existing.RouletteLastSeenWindowHours = wsc.ClampRouletteLastSeenWindowHours(*input.RouletteLastSeenWindowHours)
	}
	if input.RouletteRescueEnabled != nil {
		existing.RouletteRescueEnabled = *input.RouletteRescueEnabled
	}
	if input.RouletteRescueAfterMinutes != nil {
		existing.RouletteRescueAfterMinutes = wsc.ClampRouletteRescueMinutes(*input.RouletteRescueAfterMinutes)
	}
	if err := uc.applyRouletteStrategy(workspaceID, existing, input); err != nil {
		return nil, err
	}

	switch {
	case input.ClearWorkingHours:
		existing.WorkingHours = nil
	case input.WorkingHours != nil:
		if err := input.WorkingHours.Validate(); err != nil {
			return nil, err
		}
		existing.WorkingHours = input.WorkingHours.Normalized()
	}

	switch {
	case input.ClearOutcomeCapture:
		existing.OutcomeCapture = nil
	case input.OutcomeCapture != nil:
		capture, err := wsc.MergeOutcomeCapture(existing.OutcomeCapture, input.OutcomeCapture, uc.now())
		if err != nil {
			return nil, err
		}
		existing.OutcomeCapture = capture
	}

	existing.UpdatedBy = callerID

	if err := uc.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}

func (uc *updateWorkspaceConfigOwnerUseCase) applyRouletteStrategy(workspaceID string, cfg *wsc.WorkspaceConfig, input wsc.UpdateWorkspaceConfigOwnerInput) error {
	if input.RouletteStrategy != nil {
		strategy, err := ia.ParseStrategy(*input.RouletteStrategy)
		if err != nil {
			return err
		}
		cfg.RouletteStrategy = strategy
	}
	if input.RouletteMaxOpenPerMember != nil {
		cfg.RouletteMaxOpenPerMember = *input.RouletteMaxOpenPerMember
	}
	if input.RouletteRecipients != nil {
		recipients, err := ia.NormalizeRecipients(*input.RouletteRecipients)
		if err != nil {
			return err
		}
		if err := uc.requireMembers(workspaceID, recipients); err != nil {
			return err
		}
		cfg.RouletteRecipients = recipients
	}
	return ia.ValidateRouletteRules(cfg.EffectiveRouletteStrategy(), cfg.RouletteRecipients, cfg.RouletteMaxOpenPerMember)
}

func (uc *updateWorkspaceConfigOwnerUseCase) requireMembers(workspaceID string, recipients []ia.Recipient) error {
	if len(recipients) == 0 {
		return nil
	}
	members, err := uc.wsRepo.ListMembers(workspaceID)
	if err != nil {
		return fmt.Errorf("workspace_config: reading members to check roulette recipients: %w", err)
	}
	isMember := make(map[string]bool, len(members))
	for _, m := range members {
		if m != nil {
			isMember[m.UserID] = true
		}
	}
	for _, r := range recipients {
		if !isMember[r.UserID] {
			return ia.ErrRecipientNotMember
		}
	}
	return nil
}
