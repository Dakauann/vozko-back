package calls_query_usecase

import "vozko/domain/calls/billing"

type billingQueryUseCase struct {
	repo billing.Repository
}

func NewBillingQueryUseCase(repo billing.Repository) billing.QueryUseCase {
	return &billingQueryUseCase{repo: repo}
}

func (uc *billingQueryUseCase) GetByCallID(callID string) (*billing.CallBillingRecord, error) {
	return uc.repo.GetByCallID(callID)
}

func (uc *billingQueryUseCase) GetByCallIDs(callIDs []string) (map[string]*billing.CallBillingRecord, error) {
	return uc.repo.GetByCallIDs(callIDs)
}
