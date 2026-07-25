package balance_usecase

import "vozko/domain/balance"

type debitBalanceUseCase struct {
	repo balance.Repository
}

func NewDebitBalanceUseCase(repo balance.Repository) balance.DebitBalanceUseCase {
	return &debitBalanceUseCase{repo: repo}
}

func (uc *debitBalanceUseCase) Execute(input balance.DebitBalanceInput) (*balance.Transaction, error) {
	return uc.repo.DebitBalance(input)
}
