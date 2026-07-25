package balance_repository

import (
	"sync"
	"time"

	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/shared"
)

const invalidateDebounceInterval = 1 * time.Second

type CachedBalanceRepository struct {
	inner  balance.Repository
	shared cache.SharedState

	debounceMu    sync.Mutex
	lastInvalidAt map[string]time.Time
}

func NewCachedBalanceRepository(inner balance.Repository, shared cache.SharedState) balance.Repository {
	return &CachedBalanceRepository{
		inner:         inner,
		shared:        shared,
		lastInvalidAt: make(map[string]time.Time),
	}
}

func cacheKey(workspaceID string) string {
	return "balance:cache:" + workspaceID
}

func (r *CachedBalanceRepository) invalidateDebounced(workspaceID string) {
	now := time.Now()
	r.debounceMu.Lock()
	if last, ok := r.lastInvalidAt[workspaceID]; ok && now.Sub(last) < invalidateDebounceInterval {
		r.debounceMu.Unlock()
		return
	}
	r.lastInvalidAt[workspaceID] = now
	r.debounceMu.Unlock()

	_ = r.shared.Del(cacheKey(workspaceID))
}

func (r *CachedBalanceRepository) DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error) {
	tx, err := r.inner.DebitBalance(params)
	if err == nil {
		r.invalidateDebounced(params.WorkspaceID)
	}
	return tx, err
}

func (r *CachedBalanceRepository) CreditBalance(params balance.CreditBalanceInput) (*balance.Transaction, error) {
	tx, err := r.inner.CreditBalance(params)
	if err == nil {
		r.invalidateDebounced(params.WorkspaceID)
	}
	return tx, err
}

func (r *CachedBalanceRepository) Create(b *balance.Balance) error {
	return r.inner.Create(b)
}

func (r *CachedBalanceRepository) GetByWorkspaceID(workspaceID string) (*balance.Balance, error) {
	return r.inner.GetByWorkspaceID(workspaceID)
}

// ListWorkspacesBelowBalance is a cold-path query (a daily monitor), so it is not
// cached; it delegates to the underlying repository when it supports the port.
func (r *CachedBalanceRepository) ListWorkspacesBelowBalance(thresholdMicros int64) ([]balance.LowBalanceRow, error) {
	lister, ok := r.inner.(balance.LowBalanceLister)
	if !ok {
		return nil, nil
	}
	return lister.ListWorkspacesBelowBalance(thresholdMicros)
}

var _ balance.LowBalanceLister = (*CachedBalanceRepository)(nil)

func (r *CachedBalanceRepository) EnsureBalanceExists(workspaceID string, currency string) (*balance.Balance, error) {
	return r.inner.EnsureBalanceExists(workspaceID, currency)
}

func (r *CachedBalanceRepository) HasSufficientBalance(workspaceID string, amount int64) (bool, error) {
	return r.inner.HasSufficientBalance(workspaceID, amount)
}

func (r *CachedBalanceRepository) GetFullBalanceSummary(workspaceID string) (*balance.FullBalanceSummary, error) {
	return r.inner.GetFullBalanceSummary(workspaceID)
}

func (r *CachedBalanceRepository) GetTransaction(transactionID string) (*balance.Transaction, error) {
	return r.inner.GetTransaction(transactionID)
}

func (r *CachedBalanceRepository) ListTransactions(input balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	return r.inner.ListTransactions(input)
}

func (r *CachedBalanceRepository) ExistsTransactionByReferenceID(referenceID string) (bool, error) {
	return r.inner.ExistsTransactionByReferenceID(referenceID)
}

func (r *CachedBalanceRepository) AggregateDailyCosts(date time.Time) ([]balance.DailyCostRow, error) {
	return r.inner.AggregateDailyCosts(date)
}

func (r *CachedBalanceRepository) AggregateWhatsAppTemplateCharges(filter balance.WhatsAppChargeFilter) (*balance.WhatsAppChargeStats, error) {
	agg, ok := r.inner.(balance.WhatsAppChargeAggregator)
	if !ok {
		return &balance.WhatsAppChargeStats{}, nil
	}
	return agg.AggregateWhatsAppTemplateCharges(filter)
}

var _ balance.WhatsAppChargeAggregator = (*CachedBalanceRepository)(nil)
