package balance

import (
	"errors"
	"time"
)

var (
	ErrBalanceNotFound     = errors.New("balance not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	// ErrPriceUnavailable is a chargeable action with no configured price.
	//
	// It exists because the alternative was returning (nil, nil) — success with
	// no charge — which every caller read as "billed" and none read as "this went
	// out for free". An unpriced workspace must STOP, not send.
	ErrPriceUnavailable           = errors.New("no price configured for this service")
	ErrInvalidAmount              = errors.New("amount must be positive")
	ErrTransactionNotFound        = errors.New("transaction not found")
	ErrWorkspaceAlreadyHasBalance = errors.New("workspace already has a balance record")
	ErrInvalidResourceType        = errors.New("invalid resource type")
)

type ServiceType string

const (
	ServiceVoiceCampaign    ServiceType = "voice_campaign"
	ServiceVoiceCall        ServiceType = "voice_call"
	ServiceWhatsAppCampaign ServiceType = "whatsapp_campaign"
	// ServiceWhatsAppConversation is RESERVED and has no producer. Nothing in the
	// tree writes a transaction with it, so the ledger cannot answer any question
	// about session message volume. Anyone reporting on service messages has to
	// count conversation_messages instead; see
	// domain/analytics/service_message_exposure.go and the rule in
	// conversation.MessageType.IsMetaServiceBillable.
	//
	// It was named for outbound session messages on 360dialog numbers, funded by
	// the platform through the partner credit line. The clause that used to sit
	// here claiming Meta charges the customer directly on Meta-direct numbers was
	// wrong and is deliberately removed: we fund those too, which is what makes
	// Meta's 1 October 2026 service message charge our cost and not the
	// customer's. Inbound is never billed by anyone.
	ServiceWhatsAppConversation ServiceType = "whatsapp_conversation"
	ServiceAI                   ServiceType = "ai"
	ServiceManualAdjustment     ServiceType = "manual_adjustment"
	ServiceTopUp                ServiceType = "top_up"
	ServiceAddon                ServiceType = "addon"
)

func (s ServiceType) IsValid() bool {
	switch s {
	case ServiceVoiceCampaign, ServiceVoiceCall, ServiceWhatsAppCampaign, ServiceWhatsAppConversation, ServiceAI, ServiceManualAdjustment, ServiceTopUp, ServiceAddon:
		return true
	default:
		return false
	}
}

func AllServiceTypes() []ServiceType {
	return []ServiceType{
		ServiceVoiceCampaign,
		ServiceVoiceCall,
		ServiceWhatsAppCampaign,
		ServiceWhatsAppConversation,
		ServiceAI,
		ServiceManualAdjustment,
		ServiceTopUp,
		ServiceAddon,
	}
}

type ResourceType string

const (
	ResourceTypeMoney ResourceType = "money"
)

func (r ResourceType) IsValid() bool {
	return r == ResourceTypeMoney
}

type TransactionType string

const (
	TransactionTypeCredit TransactionType = "credit"
	TransactionTypeDebit  TransactionType = "debit"
)

type Balance struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Amount      int64     `json:"amount"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Transaction struct {
	ID                 string          `json:"id"`
	BalanceID          string          `json:"balanceId"`
	WorkspaceID        string          `json:"workspaceId"`
	Type               TransactionType `json:"type"`
	Amount             int64           `json:"amount"`
	ResourceType       ResourceType    `json:"resourceType"`
	BalanceBefore      int64           `json:"balanceBefore"`
	BalanceAfter       int64           `json:"balanceAfter"`
	ServiceType        ServiceType     `json:"serviceType"`
	ReferenceID        *string         `json:"referenceId,omitempty"`
	Description        string          `json:"description"`
	CostMicros         int64           `json:"costMicros"`
	ProfitMicros       int64           `json:"profitMicros"`
	ExchangeRateMicros int64           `json:"exchangeRateMicros"`
	IsRefund           bool            `json:"isRefund"`
	CreatedAt          time.Time       `json:"createdAt"`
}

type FullBalanceSummary struct {
	Balance           *Balance `json:"balance"`
	TotalMoneyCredits int64    `json:"totalMoneyCredits"`
	TotalMoneyDebits  int64    `json:"totalMoneyDebits"`
}

func NewBalance(id, workspaceID string, initialAmount int64, currency string) *Balance {
	now := time.Now()
	return &Balance{
		ID:          id,
		WorkspaceID: workspaceID,
		Amount:      initialAmount,
		Currency:    currency,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func NewTransaction(id, balanceID, workspaceID string, txType TransactionType, amount int64, resourceType ResourceType, balanceBefore, balanceAfter int64, serviceType ServiceType, referenceID *string, description string) *Transaction {
	return &Transaction{
		ID:            id,
		BalanceID:     balanceID,
		WorkspaceID:   workspaceID,
		Type:          txType,
		Amount:        amount,
		ResourceType:  resourceType,
		BalanceBefore: balanceBefore,
		BalanceAfter:  balanceAfter,
		ServiceType:   serviceType,
		ReferenceID:   referenceID,
		Description:   description,
		CreatedAt:     time.Now(),
	}
}
