package invoice

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvoiceNotFound            = errors.New("invoice not found")
	ErrInvalidAmount              = errors.New("amount must be positive")
	ErrInvalidPurpose             = errors.New("invalid invoice purpose")
	ErrPlanDefinitionRequired     = errors.New("plan definition is required for subscription invoice")
	ErrActiveSubscriptionRequired = errors.New("active workspace subscription required to recharge balance")
	// ErrCustomerDocumentRequired is returned when the user has no CPF/CNPJ on file. Asaas cannot
	// create a charge for a customer without a document, so we reject up front with a clear 4xx
	// (and a stable machine code) instead of letting it surface as an opaque 500.
	ErrCustomerDocumentRequired = errors.New("CPF/CNPJ obrigatório para gerar a cobrança")
)

type Purpose string

const (
	PurposeTopUp        Purpose = "TOP_UP"
	PurposeSubscription Purpose = "SUBSCRIPTION"
	// PurposeMonthlyBilling is the unified monthly charge (plan + active channel addons) on the
	// workspace billing anchor. Only its plan portion becomes saldo (see Invoice.CreditableUSD);
	// the channel portion is a vendor-cost repasse and is not credited.
	PurposeMonthlyBilling Purpose = "MONTHLY_BILLING"
)

func (p Purpose) Normalize() Purpose {
	normalized := strings.ToUpper(strings.TrimSpace(string(p)))
	if normalized == "" {
		return PurposeTopUp
	}
	return Purpose(normalized)
}

func (p Purpose) Valid() bool {
	switch p.Normalize() {
	case PurposeTopUp, PurposeSubscription, PurposeMonthlyBilling:
		return true
	default:
		return false
	}
}

// LineItemKind labels a line of a unified monthly invoice for the customer-facing breakdown.
type LineItemKind string

const (
	LineItemPlan    LineItemKind = "PLAN"
	LineItemChannel LineItemKind = "CHANNEL"
)

// InvoiceLineItem is one customer-facing line of a unified monthly invoice. It carries the final PRICE
// only, never our cost. Creditable marks the plan line, whose amount becomes saldo on payment; channel
// lines are a vendor-cost pass-through and are not credited.
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
	StatusExpired   Status = "EXPIRED"
)

type Invoice struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspaceId"`
	UserID           string  `json:"userId"`
	Purpose          Purpose `json:"purpose"`
	PlanDefinitionID *string `json:"planDefinitionId,omitempty"`
	AmountBRL        float64 `json:"amountBRL"`
	AmountUSD        int64   `json:"amountUSD"`
	// CreditableUSD is the portion of AmountUSD that becomes saldo when the invoice is paid. For
	// TOP_UP and SUBSCRIPTION it equals AmountUSD; for MONTHLY_BILLING it is only the plan portion,
	// so channel-license repasse is never credited as saldo. A zero value means "credit the full
	// AmountUSD" for backward compatibility with rows created before this field existed.
	CreditableUSD int64   `json:"creditableUSD"`
	ExchangeRate  float64 `json:"exchangeRate"`
	Status        Status  `json:"status"`
	BillingType   string  `json:"billingType"`
	BillingCycle  string  `json:"billingCycle"`
	ExternalID    string  `json:"externalId"`
	// IdempotencyKey is a caller-supplied deterministic key (e.g. monthly:{workspace}:{anchor}) that
	// makes invoice creation idempotent: create_invoice returns the existing invoice for a repeated
	// key instead of charging Asaas again. Empty for invoices that do not need it. A partial unique
	// index enforces uniqueness only for non-empty keys.
	IdempotencyKey string  `json:"idempotencyKey,omitempty"`
	PixQrCode      *string `json:"pixQrCode,omitempty"`
	PixCopy        *string `json:"pixCopy,omitempty"`
	BankSlipUrl    *string `json:"bankSlipUrl,omitempty"`
	InvoiceUrl     *string `json:"invoiceUrl,omitempty"`
	PaidAt         *int64  `json:"paidAt,omitempty"`
	Description    string  `json:"description"`
	// DueDate is when payment is due (the billing anchor for MONTHLY_BILLING). Nil for ad-hoc invoices.
	DueDate *time.Time `json:"dueDate,omitempty"`
	// LineItems is the customer-facing breakdown (plan + channel lines) of a unified monthly invoice,
	// price only. Empty for single-amount invoices (TOP_UP, SUBSCRIPTION).
	LineItems []InvoiceLineItem `json:"lineItems,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

func (i *Invoice) NormalizedPurpose() Purpose {
	if i == nil {
		return PurposeTopUp
	}
	return i.Purpose.Normalize()
}
