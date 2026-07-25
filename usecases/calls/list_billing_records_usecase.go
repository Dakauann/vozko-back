package calls_usecase

import (
	"vozko/domain/calls/billing"
	"vozko/domain/shared"
)

type listBillingRecordsUseCase struct {
	repo billing.Repository
}

func NewListBillingRecordsUseCase(repo billing.Repository) billing.ListBillingRecordsUseCase {
	return &listBillingRecordsUseCase{repo: repo}
}

func (uc *listBillingRecordsUseCase) Execute(input billing.ListBillingInput) (*shared.PaginatedResult[*billing.CallBillingRecord], error) {
	return uc.repo.ListByWorkspaceID(input)
}
