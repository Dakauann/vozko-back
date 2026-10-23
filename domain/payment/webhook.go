package payment

type WebhookEvent struct {
	ID       string         `json:"id"`
	Event    string         `json:"event"`
	Payment  WebhookPayment `json:"payment"`
	Provider Provider       `json:"provider,omitempty"`
}

type WebhookPayment struct {
	ID                string  `json:"id"`
	ExternalReference string  `json:"externalReference"`
	BillingType       string  `json:"billingType"`
	Value             float64 `json:"value"`
	Status            string  `json:"status"`

	InvoiceNumber string `json:"invoiceNumber"`

	ReversedValue float64 `json:"-"`
}

const (
	EventPaymentCreated           = "PAYMENT_CREATED"
	EventPaymentConfirmed         = "PAYMENT_CONFIRMED"
	EventPaymentReceived          = "PAYMENT_RECEIVED"
	EventPaymentReceivedInCash    = "PAYMENT_RECEIVED_IN_CASH"
	EventPaymentOverdue           = "PAYMENT_OVERDUE"
	EventPaymentRefunded          = "PAYMENT_REFUNDED"
	EventPaymentPartiallyRefunded = "PAYMENT_PARTIALLY_REFUNDED"
	EventPaymentRefundInProgress  = "PAYMENT_REFUND_IN_PROGRESS"
	EventPaymentDeleted           = "PAYMENT_DELETED"
	EventPaymentAuthorized        = "PAYMENT_AUTHORIZED"

	EventPaymentRejected   = "PAYMENT_REJECTED"
	EventPaymentChargeback = "PAYMENT_CHARGEBACK"
	EventPaymentInAnalysis = "PAYMENT_IN_ANALYSIS"

	EventPaymentChargebackRequested        = "PAYMENT_CHARGEBACK_REQUESTED"
	EventPaymentChargebackDispute          = "PAYMENT_CHARGEBACK_DISPUTE"
	EventPaymentAwaitingChargebackReversal = "PAYMENT_AWAITING_CHARGEBACK_REVERSAL"
	EventPaymentReceivedInCashUndone       = "PAYMENT_RECEIVED_IN_CASH_UNDONE"
	EventPaymentInMediation                = "PAYMENT_IN_MEDIATION"
	EventPaymentChargebackReversed         = "PAYMENT_CHARGEBACK_REVERSED"
)

type AsaasWebhookEvent = WebhookEvent

type AsaasWebhookPayment = WebhookPayment
