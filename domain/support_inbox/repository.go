package support_inbox

import "vozko/domain/shared"

type Repository interface {
	Create(inbox *SupportInbox) error
	Update(id string, inbox *SupportInbox) error
	Delete(id string) error
	FindByID(id string) (*SupportInbox, error)
	List(input ListInboxesInput) (*shared.PaginatedResult[*SupportInbox], error)
}

type ListInboxesInput struct {
	WorkspaceID string
	Search      string
	Archived    *bool
	Options     shared.QueryOptions
}

type CreateInboxUseCase interface {
	Execute(inbox *SupportInbox) (*SupportInbox, error)
}

type UpdateInboxUseCase interface {
	Execute(id string, inbox *SupportInbox) (*SupportInbox, error)
}

type DeleteInboxUseCase interface {
	Execute(id string) error
}

type GetInboxUseCase interface {
	Execute(id string) (*SupportInbox, error)
}

type ListInboxesUseCase interface {
	Execute(input ListInboxesInput) (*shared.PaginatedResult[*SupportInbox], error)
}
