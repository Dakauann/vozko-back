package workspace_phone_access_usecase

import (
	"vozko/domain/workspace_phone_access"

	"github.com/google/uuid"
)

type grantAccessUseCase struct {
	repo workspace_phone_access.Repository
}

func NewGrantAccessUseCase(repo workspace_phone_access.Repository) workspace_phone_access.GrantAccessUseCase {
	return &grantAccessUseCase{repo: repo}
}

func (uc *grantAccessUseCase) Execute(input workspace_phone_access.GrantAccessInput) (*workspace_phone_access.WorkspacePhoneAccess, error) {
	access := workspace_phone_access.NewWorkspacePhoneAccess(
		uuid.New().String(),
		input.WorkspaceID,
		input.PhoneID,
		input.GrantedBy,
	)

	if err := uc.repo.Create(access); err != nil {
		return nil, err
	}

	return access, nil
}
