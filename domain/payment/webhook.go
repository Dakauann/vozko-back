package payment

// WebhookEvent is the canonical, provider-agnostic payment notification the business
// handler works on. Both provider consumers produce it: the Asaas consumer unmarshals
// it straight from the webhook body (the JSON tags match Asaas's envelope), while the
// Mercado Pago consumer builds it after fetching the payment, because Mercado Pago's
// notification carries only a resource id.
//
// Event uses the Asaas event vocabulary ("PAYMENT_RECEIVED", "PAYMENT_OVERDUE", ...)
// as the canonical one. That is a deliberate choice over inventing a third vocabulary:
// it keeps the existing, heavily-tested handler untouched, and adapters translate into
// it in exactly one place per provider (see EventFor* in each adapter).
type WebhookEvent struct {
	ID      string         `json:"id"`
	Event   string         `json:"event"`
	Payment WebhookPayment `json:"payment"`
	// Provider labels which gateway produced the event. Empty means Asaas, for
	// backward compatibility with events already queued when this field was added.
	Provider Provider `json:"provider,omitempty"`
}

type WebhookPayment struct {
	ID                string  `json:"id"`
	ExternalReference string  `json:"externalReference"`
	BillingType       string  `json:"billingType"`
	Value             float64 `json:"value"`
	Status            string  `json:"status"`

	InvoiceNumber string `json:"invoiceNumber"`
}

// Canonical webhook event names. The PAYMENT_* names Asaas already sent are reused
// verbatim; the last three exist because Mercado Pago reports states Asaas has no
// event for, and are handled explicitly rather than being silently coerced.
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

	// EventPaymentRejected is terminal: the provider refused the charge and it can
	// never be paid. Mercado Pago's "rejected" status maps here.
	EventPaymentRejected = "PAYMENT_REJECTED"
	// EventPaymentChargeback is money already credited being pulled back. It is
	// handled exactly like a refund so the balance ledger stays honest.
	EventPaymentChargeback = "PAYMENT_CHARGEBACK"
	// EventPaymentInAnalysis is informational: the charge is under provider review
	// and no local state should move yet. Handlers deliberately ignore it.
	EventPaymentInAnalysis = "PAYMENT_IN_ANALYSIS"
)

// AsaasWebhookEvent is the pre-multi-provider name for WebhookEvent.
//
// Deprecated: use WebhookEvent. Kept as an alias so existing call sites and the
// Asaas webhook test suite continue to compile against the canonical type.
type AsaasWebhookEvent = WebhookEvent

// AsaasWebhookPayment is the pre-multi-provider name for WebhookPayment.
//
// Deprecated: use WebhookPayment.
type AsaasWebhookPayment = WebhookPayment
