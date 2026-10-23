package container

import (
	"vozko/domain/balance"
	balance_usecase "vozko/usecases/balance"
	workspace_pricing_usecase "vozko/usecases/workspace_pricing"
)

func (c *Container) walletRate() balance.ExchangeRate {
	return balance_usecase.NewExchangeRateSource(workspace_pricing_usecase.NewGetExchangeRateUseCase(c.repositories.workspacePricing))
}
