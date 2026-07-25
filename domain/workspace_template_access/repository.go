package workspace_template_access

import "vozko/domain/shared"

type ListWorkspaceAccessInput struct {
	WorkspaceID string
	shared.QueryOptions
}

type ListTemplateAccessInput struct {
	TemplateID string
	shared.QueryOptions
}

type Repository interface {
	Create(access *WorkspaceTemplateAccess) error
	Delete(id string) error
	DeleteByWorkspaceAndTemplate(workspaceID, templateID string) error

	FindByID(id string) (*WorkspaceTemplateAccess, error)
	FindByWorkspaceAndTemplate(workspaceID, templateID string) (*WorkspaceTemplateAccess, error)

	ListByWorkspace(input ListWorkspaceAccessInput) (*shared.PaginatedResult[*WorkspaceTemplateAccess], error)
	ListByTemplate(input ListTemplateAccessInput) (*shared.PaginatedResult[*WorkspaceTemplateAccess], error)

	GetTemplateIDsForWorkspace(workspaceID string) ([]string, error)
	HasAccess(workspaceID, templateID string) (bool, error)
}
