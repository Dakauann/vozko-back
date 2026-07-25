package support_inbox_usecase

import (
	"vozko/domain/shared"
	si "vozko/domain/support_inbox"
)

type listInboxesUseCase struct {
	repo si.Repository
}

func NewListInboxesUseCase(repo si.Repository) si.ListInboxesUseCase {
	return &listInboxesUseCase{repo: repo}
}

func (uc *listInboxesUseCase) Execute(input si.ListInboxesInput) (*shared.PaginatedResult[*si.SupportInbox], error) {
	return uc.repo.List(input)
}
