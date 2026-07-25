package workspace_template_access_usecase

import (
	"github.com/google/uuid"

	"vozko/domain/workspace_template_access"
)

type grantAccessUseCase struct {
	repo workspace_template_access.Repository
}

func NewGrantAccessUseCase(repo workspace_template_access.Repository) workspace_template_access.GrantAccessUseCase {
	return &grantAccessUseCase{repo: repo}
}

func (uc *grantAccessUseCase) Execute(input workspace_template_access.GrantAccessInput) (*workspace_template_access.WorkspaceTemplateAccess, error) {
	access := workspace_template_access.NewWorkspaceTemplateAccess(
		uuid.New().String(),
		input.WorkspaceID,
		input.TemplateID,
		input.GrantedBy,
	)

	if err := uc.repo.Create(access); err != nil {
		return nil, err
	}

	return access, nil
}
