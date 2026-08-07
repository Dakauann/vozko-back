package workspace_config_usecase

import (
	"context"
	"vozko/domain/user"
	wsc "vozko/domain/workspace_config"
)

type updateWorkspaceConfigUseCase struct {
	repo wsc.Repository
}

func NewUpdateWorkspaceConfigUseCase(repo wsc.Repository) wsc.UpdateWorkspaceConfigUseCase {
	return &updateWorkspaceConfigUseCase{repo: repo}
}

func (uc *updateWorkspaceConfigUseCase) Execute(ctx context.Context, workspaceID, userID, userRole string, input wsc.UpdateWorkspaceConfigInput) (*wsc.WorkspaceConfig, error) {
	if userRole != string(user.RoleAdmin) {
		return nil, wsc.ErrUnauthorized
	}

	// TODO: create a history of changes, tracking who made the action
	existing, err := uc.repo.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	if input.CampaignSpamProtectionDays != nil && *input.CampaignSpamProtectionDays >= 0 {
		existing.CampaignSpamProtectionDays = *input.CampaignSpamProtectionDays
	}

	// The unofficial-WhatsApp allowance. Reachable ONLY here: the workspace-facing
	// update takes a different input type that has no such field, and the role
	// check above is re-asserted by the router's RoleAdmin guard. Both matter —
	// this grants capacity on hosts we pay for, on a channel where a connected
	// number can get a customer banned.
	//
	// A negative value is REJECTED rather than clamped: it is a typo, and quietly
	// turning it into zero would revoke a workspace's whole allowance while
	// reporting success.
	if input.IncludedUnofficialWhatsAppInstances != nil {
		granted := *input.IncludedUnofficialWhatsAppInstances
		if granted < 0 {
			return nil, wsc.ErrInvalidIncludedInstances
		}
		if granted > wsc.MaxIncludedUnofficialWhatsAppInstances {
			granted = wsc.MaxIncludedUnofficialWhatsAppInstances
		}
		existing.IncludedUnofficialWhatsAppInstances = granted
	}

	existing.UpdatedBy = userID

	if err := uc.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}
