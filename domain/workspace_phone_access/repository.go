package workspace_phone_access

import "vozko/domain/shared"

type ListWorkspaceAccessInput struct {
	WorkspaceID string
	shared.QueryOptions
}

type ListPhoneAccessInput struct {
	PhoneID string
	shared.QueryOptions
}

type Repository interface {
	Create(access *WorkspacePhoneAccess) error
	Delete(id string) error
	DeleteByWorkspaceAndPhone(workspaceID, phoneID string) error

	FindByID(id string) (*WorkspacePhoneAccess, error)
	FindByWorkspaceAndPhone(workspaceID, phoneID string) (*WorkspacePhoneAccess, error)

	ListByWorkspace(input ListWorkspaceAccessInput) (*shared.PaginatedResult[*WorkspacePhoneAccess], error)
	ListByPhone(input ListPhoneAccessInput) (*shared.PaginatedResult[*WorkspacePhoneAccess], error)

	GetPhoneIDsForWorkspace(workspaceID string) ([]string, error)
	HasAccess(workspaceID, phoneID string) (bool, error)
}
