package balance_usecase

import (
	"github.com/google/uuid"

	"vozko/domain/balance"
)

type createBalanceUseCase struct {
	repo balance.Repository
}

func NewCreateBalanceUseCase(repo balance.Repository) balance.CreateBalanceUseCase {
	return &createBalanceUseCase{repo: repo}
}

func (uc *createBalanceUseCase) Execute(input balance.CreateBalanceInput) (*balance.Balance, error) {
	if input.InitialAmount < 0 {
		return nil, balance.ErrInvalidAmount
	}

	currency := input.Currency
	if currency == "" {
		currency = "USD"
	}

	b := balance.NewBalance(uuid.New().String(), input.WorkspaceID, input.InitialAmount, currency)
	if err := uc.repo.Create(b); err != nil {
		return nil, err
	}

	return b, nil
}
