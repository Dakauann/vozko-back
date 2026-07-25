package workspace_template_access

import "vozko/domain/shared"

type GrantAccessInput struct {
	WorkspaceID string
	TemplateID  string
	GrantedBy   string
}

type RevokeAccessInput struct {
	WorkspaceID string
	TemplateID  string
}

type GrantAccessUseCase interface {
	Execute(input GrantAccessInput) (*WorkspaceTemplateAccess, error)
}

type RevokeAccessUseCase interface {
	Execute(input RevokeAccessInput) error
}

type ListWorkspaceAccessUseCase interface {
	Execute(input ListWorkspaceAccessInput) (*shared.PaginatedResult[*WorkspaceTemplateAccess], error)
}

type ListTemplateAccessUseCase interface {
	Execute(input ListTemplateAccessInput) (*shared.PaginatedResult[*WorkspaceTemplateAccess], error)
}

type CheckAccessUseCase interface {
	Execute(workspaceID, templateID string) (bool, error)
}
