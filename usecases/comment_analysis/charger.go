package comment_analysis_usecase

import (
	"context"
	"fmt"
	"time"

	"vozko/domain/balance"
	"vozko/domain/cache"
	ca "vozko/domain/comment_analysis"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

// Billing (plan §9). Token usage is billed by the AI adapter the moment
// WorkspaceID is on the call; nothing here touches that. This covers the
// two things on top of it: the optional per-comment surcharge, debited once
// per batch, and the daily cap that stops a viral night from producing a
// bill nobody authorised.

const (
	batchReferencePrefix = "cabatch:"
	dailyCapKeyPrefix    = "comment_analysis:spend:"
	dailyCapTTL          = 48 * time.Hour
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

// ReserveDaily claims items against the workspace's cap for the UTC day.
// TryIncrBy is atomic, so two replicas cannot both squeeze under the cap.
func (c *charger) ReserveDaily(_ context.Context, workspaceID string, items, cap int, day time.Time) (bool, error) {
	if items <= 0 {
		return true, nil
	}
	if cap <= 0 {
		cap = ca.DefaultDailyCap
	}
	key := dailyCapKeyPrefix + workspaceID + ":" + day.UTC().Format("2006-01-02")
	ok, err := c.state.TryIncrBy(key, int64(items), int64(cap))
	if err != nil {
		return false, err
	}
	// Best effort: the key names the day, so a missing TTL only leaves a
	// small key behind, never a wrong count.
	_, _ = c.state.Expire(key, dailyCapTTL)
	return ok, nil
}

// ChargeBatch debits the surcharge once per batch, idempotent on the batch
// id. A configured price of 0 is "token billing only" and returns 0 with no
// error; it must not take the ErrPriceUnavailable path a telephony rate
// would.
func (c *charger) ChargeBatch(_ context.Context, workspaceID, batchID string, items int) (int64, error) {
	if c.pricer == nil || c.balances == nil || items <= 0 {
		return 0, nil
	}
	price, err := c.pricer.PriceCommentAnalysis(workspaceID, items)
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
		ServiceType:  balance.ServiceCommentAnalysis,
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
