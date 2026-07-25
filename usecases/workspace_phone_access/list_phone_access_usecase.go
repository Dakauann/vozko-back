package workspace_phone_access_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workspace_phone_access"
)

type listPhoneAccessUseCase struct {
	repo workspace_phone_access.Repository
}

func NewListPhoneAccessUseCase(repo workspace_phone_access.Repository) workspace_phone_access.ListPhoneAccessUseCase {
	return &listPhoneAccessUseCase{repo: repo}
}

func (uc *listPhoneAccessUseCase) Execute(input workspace_phone_access.ListPhoneAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	return uc.repo.ListByPhone(input)
}
