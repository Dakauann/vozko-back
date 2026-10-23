package invoice

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/payment"
)

var (
	ErrInvoiceNotFound            = errors.New("invoice not found")
	ErrInvalidAmount              = errors.New("amount must be positive")
	ErrInvalidPurpose             = errors.New("invalid invoice purpose")
	ErrPlanDefinitionRequired     = errors.New("plan definition is required for subscription invoice")
	ErrActiveSubscriptionRequired = errors.New("active workspace subscription required to recharge balance")
	ErrCustomerDocumentRequired   = errors.New("CPF/CNPJ obrigatório para gerar a cobrança")
	ErrBillingAddressRequired     = errors.New("endereço obrigatório para gerar boleto")
	ErrTopUpBelowMinimum          = errors.New("top-up amount is below the minimum")
	ErrTopUpAboveMaximum          = errors.New("top-up amount is above the maximum")
	ErrInvalidIdempotencyKey      = errors.New("idempotency key is malformed")
	ErrUnknownPurpose             = errors.New("invoice has no known purpose to settle")
)

type Purpose string

const (
	PurposeTopUp          Purpose = "TOP_UP"
	PurposeSubscription   Purpose = "SUBSCRIPTION"
	PurposeMonthlyBilling Purpose = "MONTHLY_BILLING"
)

func (p Purpose) Normalize() Purpose {
	return Purpose(strings.ToUpper(strings.TrimSpace(string(p))))
}

func (p Purpose) Valid() bool {
	switch p.Normalize() {
	case PurposeTopUp, PurposeSubscription, PurposeMonthlyBilling:
		return true
	default:
		return false
	}
}

type LineItemKind string

const (
	LineItemPlan    LineItemKind = "PLAN"
	LineItemChannel LineItemKind = "CHANNEL"
)

type InvoiceLineItem struct {
	Kind       LineItemKind `json:"kind"`
	Label      string       `json:"label"`
	AmountBRL  float64      `json:"amountBRL"`
	Quantity   int          `json:"quantity,omitempty"`
	Prorated   bool         `json:"prorated,omitempty"`
	Creditable bool         `json:"creditable"`
}

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusPaid      Status = "PAID"
	StatusOverdue   Status = "OVERDUE"
	StatusCancelled Status = "CANCELLED"
	StatusRefunded  Status = "REFUNDED"
	StatusReversed  Status = "REVERSED"
	StatusExpired   Status = "EXPIRED"
)

func SettledStatuses() []string {
	return []string{string(StatusPaid), string(StatusRefunded), string(StatusReversed)}
}

type Invoice struct {
	ID               string            `json:"id"`
	WorkspaceID      string            `json:"workspaceId"`
	UserID           string            `json:"userId"`
	Purpose          Purpose           `json:"purpose"`
	PlanDefinitionID *string           `json:"planDefinitionId,omitempty"`
	AmountBRL        float64           `json:"amountBRL"`
	AmountUSD        int64             `json:"amountUSD"`
	CreditableUSD    int64             `json:"creditableUSD"`
	ExchangeRate     float64           `json:"exchangeRate"`
	Status           Status            `json:"status"`
	BillingType      string            `json:"billingType"`
	BillingCycle     string            `json:"billingCycle"`
	Provider         payment.Provider  `json:"provider"`
	ExternalID       string            `json:"externalId"`
	IdempotencyKey   string            `json:"idempotencyKey,omitempty"`
	PixQrCode        *string           `json:"pixQrCode,omitempty"`
	PixCopy          *string           `json:"pixCopy,omitempty"`
	BankSlipUrl      *string           `json:"bankSlipUrl,omitempty"`
	InvoiceUrl       *string           `json:"invoiceUrl,omitempty"`
	ChargeExpiresAt  *time.Time        `json:"chargeExpiresAt,omitempty"`
	PaidAt           *int64            `json:"paidAt,omitempty"`
	Description      string            `json:"description"`
	DueDate          *time.Time        `json:"dueDate,omitempty"`
	LineItems        []InvoiceLineItem `json:"lineItems,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

func (i *Invoice) NormalizedPurpose() Purpose {
	if i == nil {
		return ""
	}
	return i.Purpose.Normalize()
}

func (i *Invoice) Reserved() bool {
	return strings.TrimSpace(i.ExternalID) == ""
}
