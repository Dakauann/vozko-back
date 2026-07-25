package insurance_usecase

import (
	"context"

	"vozko/domain/insurance"
)

type listPoliciesUseCase struct{}

func NewListPoliciesUseCase() insurance.ListPoliciesUseCase {
	return listPoliciesUseCase{}
}

func (uc listPoliciesUseCase) Execute(ctx context.Context) ([]insurance.PolicySummary, error) {
	return insurance.AvailablePolicySummaries(), nil
}
