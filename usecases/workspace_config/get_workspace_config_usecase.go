package workspace_config_usecase

import (
	"context"

	wsc "vozko/domain/workspace_config"
)

type getWorkspaceConfigUseCase struct {
	repo wsc.Repository
}

func NewGetWorkspaceConfigUseCase(repo wsc.Repository) wsc.GetWorkspaceConfigUseCase {
	return &getWorkspaceConfigUseCase{repo: repo}
}

func (uc *getWorkspaceConfigUseCase) Execute(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error) {
	return uc.repo.GetByWorkspaceID(ctx, workspaceID)
}
