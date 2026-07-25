package balance

import "vozko/domain/shared"

type CreateBalanceInput struct {
	WorkspaceID   string
	InitialAmount int64
	Currency      string
}

type CreditBalanceInput struct {
	WorkspaceID string
	Amount      int64
	ServiceType ServiceType
	ReferenceID *string
	Description string

	CostMicros   int64
	ProfitMicros int64
	IsRefund     bool

	ExchangeRateMicros int64
}

type DebitBalanceInput struct {
	WorkspaceID string
	Amount      int64
	ServiceType ServiceType
	ReferenceID *string
	Description string

	CostMicros   int64
	ProfitMicros int64
	IsRefund     bool

	AllowNegative bool

	ExchangeRateMicros int64
}

type CreateBalanceUseCase interface {
	Execute(input CreateBalanceInput) (*Balance, error)
}

type GetBalanceUseCase interface {
	Execute(workspaceID string) (*Balance, error)
}

type CreditBalanceUseCase interface {
	Execute(input CreditBalanceInput) (*Transaction, error)
}

type DebitBalanceUseCase interface {
	Execute(input DebitBalanceInput) (*Transaction, error)
}

type ListTransactionsUseCase interface {
	Execute(input ListTransactionsInput) (*shared.PaginatedResult[*Transaction], error)
}

type CreditResourceUseCase interface {
	Execute(input CreditBalanceInput) (*Transaction, error)
}

type DebitResourceUseCase interface {
	Execute(input DebitBalanceInput) (*Transaction, error)
}

type ConsumeWhatsappTemplateUseCase interface {
	Execute(workspaceID string, referenceID string, templateCategory string) (*Transaction, error)
	Refund(workspaceID string, referenceID string, templateCategory string) error
	GetTemplateCostMicros(workspaceID string, templateCategory string) (int64, error)
}

type CheckBalanceUseCase interface {
	Execute(workspaceID string, amount int64) (bool, error)
}

type CachedBalanceChecker interface {
	HasSufficientBalance(workspaceID string, amountMicros int64) (bool, error)
	GetBalance(workspaceID string) (int64, error)
	Invalidate(workspaceID string)

	InvalidateDebounced(workspaceID string)
}

type PreCallBalanceChecker interface {
	CanAffordCall(workspaceID string) (bool, error)
	CanAffordAI(workspaceID string) (bool, error)
}

// TODO: Implement ConsumeVoipMinuteUseCase using pricing-based balance debit

type GetFullBalanceSummaryUseCase interface {
	Execute(workspaceID string) (*FullBalanceSummary, error)
}

type GetOrCreateBalanceUseCase interface {
	Execute(workspaceID string) (*Balance, error)
}

type GetOrCreateFullBalanceSummaryUseCase interface {
	Execute(workspaceID string) (*FullBalanceSummary, error)
}
