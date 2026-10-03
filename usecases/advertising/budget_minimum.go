package advertising

import (
	"context"

	ads "vozko/domain/advertising"
)

type minimumGateway interface {
	MinimumBudgets(ctx context.Context, token, metaAccountID string, bidAmount int64) (ads.MinimumBudgets, error)
}

type budgetFloor struct {
	access  accountAccess
	gateway minimumGateway
}

type budgetCheck struct {
	account *ads.AdAccount
	token   string
	field   string
	budget  *ads.Budget
	goal    ads.OptimizationGoal
	bid     ads.Bid
}

func (f budgetFloor) check(ctx context.Context, c budgetCheck) (*ads.BudgetMinimum, error) {
	if c.budget == nil || c.budget.Kind != ads.BudgetDaily {
		return nil, nil
	}
	minimums, err := f.gateway.MinimumBudgets(ctx, c.token, c.account.MetaAccountID, c.bid.ManualAmount())
	if err != nil {
		return nil, f.access.failed(ctx, c.account, err)
	}
	minimum, err := minimums.Check(c.field, *c.budget, c.goal)
	if err != nil {
		return nil, err
	}
	return &minimum, nil
}
