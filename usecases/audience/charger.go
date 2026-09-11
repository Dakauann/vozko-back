package audience_usecase

import (
	"context"
	"fmt"

	ca "vozko/domain/audience"
	"vozko/domain/balance"
	"vozko/domain/cache"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

// Billing (plan §9). Token usage is billed by the AI adapter the moment
// WorkspaceID is on the call; nothing here touches that. This covers the
// two things on top of it: the optional per-comment surcharge, debited once
// per batch, and the daily cap that stops a viral night from producing a
// bill nobody authorised.

const (
	batchReferencePrefix = "cabatch:"
)

type charger struct {
	balances balance.Repository
	pricer   workspace_pricing.Pricer
	state    cache.SharedState
}

// NewCharger builds the billing side of the engine. balances and pricer may
// be nil in a deployment with no surcharge; the daily cap still applies.
func NewCharger(balances balance.Repository, pricer workspace_pricing.Pricer, state cache.SharedState) ca.Charger {
	return &charger{balances: balances, pricer: pricer, state: state}
}

// ChargeBatch debits the surcharge once per batch, idempotent on the batch
// id. A configured price of 0 is "token billing only" and returns 0 with no
// error; it must not take the ErrPriceUnavailable path a telephony rate
// would.
func (c *charger) ChargeBatch(_ context.Context, workspaceID, batchID string, items int) (int64, error) {
	if c.pricer == nil || c.balances == nil || items <= 0 {
		return 0, nil
	}
	price, err := c.pricer.PriceAudience(workspaceID, items)
	if err != nil {
		return 0, fmt.Errorf("comment analysis: pricing surcharge: %w", err)
	}
	if price.PriceMicros <= 0 {
		return 0, nil
	}
	reference := batchReferencePrefix + batchID
	exists, err := c.balances.ExistsTransactionByReferenceID(reference)
	if err != nil {
		return 0, err
	}
	if exists {
		return price.PriceMicros, nil
	}
	_, err = c.balances.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:  workspaceID,
		Amount:       price.PriceMicros,
		ServiceType:  balance.ServiceAudience,
		ReferenceID:  &reference,
		Description:  fmt.Sprintf("Análise de comentários: %d comentários (lote %s)", items, batchID),
		CostMicros:   price.CostMicros,
		ProfitMicros: price.ProfitMicros,
		// The classification already happened and was token-billed; refusing
		// the surcharge now would leave the ledger inconsistent with the
		// work. The floor check before the call is what keeps balances up.
		AllowNegative: true,
	})
	if err != nil {
		return 0, err
	}
	return price.PriceMicros, nil
}
