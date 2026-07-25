package calls_cdr_usecase

import (
	"vozko/domain/calls/cdr"
	"vozko/domain/shared"
)

type getCallUseCase struct {
	repo cdr.Repository
}

func NewGetCallUseCase(repo cdr.Repository) cdr.GetCallUseCase {
	return &getCallUseCase{repo: repo}
}

func (uc *getCallUseCase) Execute(callID string) (*cdr.Call, error) {
	return uc.repo.GetByCallID(callID)
}

type listCallsUseCase struct {
	repo cdr.Repository
}

func NewListCallsUseCase(repo cdr.Repository) cdr.ListCallsUseCase {
	return &listCallsUseCase{repo: repo}
}

func (uc *listCallsUseCase) Execute(filters cdr.ListFilters) (*shared.PaginatedResult[*cdr.Call], error) {
	if filters.WorkspaceID == "" {
		return nil, cdr.ErrWorkspaceIDRequired
	}
	return uc.repo.List(filters)
}
