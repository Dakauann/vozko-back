package balance_usecase

import "vozko/domain/balance"

type debitResourceUseCase struct {
	repo balance.Repository
}

func NewDebitResourceUseCase(repo balance.Repository) balance.DebitResourceUseCase {
	return &debitResourceUseCase{repo: repo}
}

func (uc *debitResourceUseCase) Execute(input balance.DebitBalanceInput) (*balance.Transaction, error) {
	if input.Amount <= 0 {
		return nil, balance.ErrInvalidAmount
	}

	return uc.repo.DebitBalance(input)
}
