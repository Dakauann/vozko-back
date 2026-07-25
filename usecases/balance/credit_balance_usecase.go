package balance_usecase

import "vozko/domain/balance"

type creditBalanceUseCase struct {
	repo balance.Repository
}

func NewCreditBalanceUseCase(repo balance.Repository) balance.CreditBalanceUseCase {
	return &creditBalanceUseCase{repo: repo}
}

func (uc *creditBalanceUseCase) Execute(input balance.CreditBalanceInput) (*balance.Transaction, error) {
	return uc.repo.CreditBalance(input)
}
