package whatsapp_campaign_usecase

import (
	"context"

	wc "vozko/domain/whatsapp_campaign"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type assignDepartmentUseCase struct {
	campaignRepo       wc.Repository
	departmentResolver workspace_department.CreationDepartmentResolver
}

func NewAssignDepartmentUseCase(
	campaignRepo wc.Repository,
	departmentResolver workspace_department.CreationDepartmentResolver,
) wc.AssignDepartmentUseCase {
	return &assignDepartmentUseCase{
		campaignRepo:       campaignRepo,
		departmentResolver: departmentResolver,
	}
}

func (uc *assignDepartmentUseCase) Execute(ctx context.Context, campaignID string) (*wc.Campaign, error) {
	if campaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}

	current, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, wc.ErrCampaignNotFound
	}

	departmentID, err := uc.departmentResolver.Resolve(ctx, current.WorkspaceID)
	if err != nil {
		return nil, err
	}

	current.DepartmentID = departmentID
	if err := uc.campaignRepo.Update(campaignID, current); err != nil {
		return nil, err
	}

	return uc.campaignRepo.FindByID(campaignID)
}
