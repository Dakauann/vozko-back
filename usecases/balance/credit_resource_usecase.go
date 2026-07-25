package balance_usecase

import (
	"vozko/domain/balance"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

type creditResourceUseCase struct {
	repo balance.Repository
}

func NewCreditResourceUseCase(repo balance.Repository) balance.CreditResourceUseCase {
	return &creditResourceUseCase{repo: repo}
}

func (uc *creditResourceUseCase) Execute(input balance.CreditBalanceInput) (*balance.Transaction, error) {
	if input.Amount <= 0 {
		return nil, balance.ErrInvalidAmount
	}

	tx, err := uc.repo.CreditBalance(input)
	if err != nil {
		return nil, err
	}

	wc_usecase.SignalSendingsAvailable()

	return tx, nil
}
