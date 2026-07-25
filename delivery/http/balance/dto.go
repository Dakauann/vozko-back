package balance

import (
	balancedomain "vozko/domain/balance"
)

type CreateBalanceRequest struct {
	WorkspaceID   string `json:"workspaceId" example:"ws_a1b2c3"`
	InitialAmount int64  `json:"initialAmount" example:"0"`
	Currency      string `json:"currency" example:"USD"`
}

type CreditDebitRequest struct {
	Amount      int64   `json:"amount" example:"5000000"`
	ServiceType string  `json:"serviceType" example:"top_up"`
	ReferenceID *string `json:"referenceId,omitempty" example:"pay_a1b2c3"`
	Description string  `json:"description" example:"Recarga de saldo"`
}

type CreditResourceRequest struct {
	Amount      int64   `json:"amount" example:"5000000"`
	ServiceType string  `json:"serviceType" example:"top_up"`
	ReferenceID *string `json:"referenceId,omitempty" example:"pay_a1b2c3"`
	Description string  `json:"description" example:"Ajuste manual de saldo"`
}

type balanceResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type fullBalanceSummaryResponse struct {
	Balance           balanceResponse `json:"balance"`
	TotalMoneyCredits int64           `json:"totalMoneyCredits"`
	TotalMoneyDebits  int64           `json:"totalMoneyDebits"`
}

type transactionResponse struct {
	ID            string  `json:"id"`
	BalanceID     string  `json:"balanceId"`
	WorkspaceID   string  `json:"workspaceId"`
	Type          string  `json:"type"`
	Amount        int64   `json:"amount"`
	ResourceType  string  `json:"resourceType"`
	BalanceBefore int64   `json:"balanceBefore"`
	BalanceAfter  int64   `json:"balanceAfter"`
	ServiceType   string  `json:"serviceType"`
	ReferenceID   *string `json:"referenceId,omitempty"`
	Description   string  `json:"description"`
	CreatedAt     string  `json:"createdAt"`
}

func mapBalanceToResponse(b *balancedomain.Balance) balanceResponse {
	return balanceResponse{
		ID:          b.ID,
		WorkspaceID: b.WorkspaceID,
		Amount:      b.Amount,
		Currency:    b.Currency,
		CreatedAt:   b.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   b.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func mapFullBalanceSummaryToResponse(s *balancedomain.FullBalanceSummary) fullBalanceSummaryResponse {
	return fullBalanceSummaryResponse{
		Balance:           mapBalanceToResponse(s.Balance),
		TotalMoneyCredits: s.TotalMoneyCredits,
		TotalMoneyDebits:  s.TotalMoneyDebits,
	}
}

func mapTransactionToResponse(t *balancedomain.Transaction) transactionResponse {
	return transactionResponse{
		ID:            t.ID,
		BalanceID:     t.BalanceID,
		WorkspaceID:   t.WorkspaceID,
		Type:          string(t.Type),
		Amount:        t.Amount,
		ResourceType:  string(t.ResourceType),
		BalanceBefore: t.BalanceBefore,
		BalanceAfter:  t.BalanceAfter,
		ServiceType:   string(t.ServiceType),
		ReferenceID:   t.ReferenceID,
		Description:   t.Description,
		CreatedAt:     t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
