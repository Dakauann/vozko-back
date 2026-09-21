package balance

import (
	"time"

	"vozko/domain/shared"
)

type ListTransactionsInput struct {
	WorkspaceID  string
	ServiceType  *ServiceType
	Type         *TransactionType
	ResourceType *ResourceType
	StartDate    *time.Time
	EndDate      *time.Time
	shared.QueryOptions
}

type DailyCostRow struct {
	WorkspaceID  string
	ServiceType  string
	CostMicros   int64
	MessageCount int
}

type LowBalanceRow struct {
	WorkspaceID  string
	AmountMicros int64
	Currency     string
}

type LowBalanceLister interface {
	ListWorkspacesBelowBalance(thresholdMicros int64) ([]LowBalanceRow, error)
}

type Repository interface {
	Create(balance *Balance) error
	GetByWorkspaceID(workspaceID string) (*Balance, error)

	EnsureBalanceExists(workspaceID string, currency string) (*Balance, error)

	CreditBalance(params CreditBalanceInput) (*Transaction, error)
	DebitBalance(params DebitBalanceInput) (*Transaction, error)

	HasSufficientBalance(workspaceID string, amount int64) (bool, error)

	GetFullBalanceSummary(workspaceID string) (*FullBalanceSummary, error)

	GetTransaction(transactionID string) (*Transaction, error)
	ListTransactions(input ListTransactionsInput) (*shared.PaginatedResult[*Transaction], error)
	ExistsTransactionByReferenceID(referenceID string) (bool, error)

	AggregateDailyCosts(date time.Time) ([]DailyCostRow, error)
}
