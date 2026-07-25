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

	existing.UpdatedBy = userID

	if err := uc.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}
