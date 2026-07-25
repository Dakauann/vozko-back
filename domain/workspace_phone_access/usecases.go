package workspace_phone_access

import "vozko/domain/shared"

type GrantAccessInput struct {
	WorkspaceID string
	PhoneID     string
	GrantedBy   string
}

type RevokeAccessInput struct {
	WorkspaceID string
	PhoneID     string
}

type GrantAccessUseCase interface {
	Execute(input GrantAccessInput) (*WorkspacePhoneAccess, error)
}

type RevokeAccessUseCase interface {
	Execute(input RevokeAccessInput) error
}

type ListWorkspaceAccessUseCase interface {
	Execute(input ListWorkspaceAccessInput) (*shared.PaginatedResult[*WorkspacePhoneAccess], error)
}

type ListPhoneAccessUseCase interface {
	Execute(input ListPhoneAccessInput) (*shared.PaginatedResult[*WorkspacePhoneAccess], error)
}

type CheckAccessUseCase interface {
	Execute(workspaceID, phoneID string) (bool, error)
}
