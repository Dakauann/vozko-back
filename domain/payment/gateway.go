package payment

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Provider identifies which payment gateway backs the deployment. It is chosen once
// at boot from PAYMENT_PROVIDER; nothing downstream branches on it for business rules,
// only for wiring and for labelling stored/observed data.
type Provider string

const (
	ProviderAsaas       Provider = "asaas"
	ProviderMercadoPago Provider = "mercadopago"
)

// ParseProvider normalizes an operator-supplied provider name. "mercado_pago",
// "mercado-pago" and "mp" are accepted spellings because they are what people type.
func ParseProvider(raw string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, " ", ""))) {
	case "", string(ProviderAsaas):
		return ProviderAsaas, nil
	case string(ProviderMercadoPago), "mercado_pago", "mercado-pago", "mp":
		return ProviderMercadoPago, nil
	default:
		return "", ErrUnknownProvider
	}
}

// Method is the provider-agnostic charge method. Adapters translate it to whatever
// their API calls it ("PIX"/"BOLETO" for Asaas, "pix"/"bolbradesco" for Mercado Pago).
type Method string

const (
	MethodPix        Method = "PIX"
	MethodBoleto     Method = "BOLETO"
	MethodCreditCard Method = "CREDIT_CARD"
)

// NormalizeMethod maps free-form billing type strings (as stored on invoices and
// submitted by API clients) onto the canonical set. Unknown values fall back to PIX,
// which is the only method every supported provider can always issue.
func NormalizeMethod(raw string) Method {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "BOLETO", "BANK_SLIP", "BOLBRADESCO":
		return MethodBoleto
	case "CREDIT_CARD", "CREDITCARD", "CARD":
		return MethodCreditCard
	default:
		return MethodPix
	}
}

// GatewayCustomer is the payer identity a charge is issued against.
//
// Document (CPF/CNPJ, digits only) is mandatory for both providers in Brazil: Asaas
// keys its customer records on it, and Mercado Pago requires payer.identification for
// PIX and boleto. Email is mandatory for Mercado Pago and ignored by Asaas.
type GatewayCustomer struct {
	Name     string
	Email    string
	Document string
	Address  *GatewayAddress
}

// GatewayAddress is only consulted for boleto on Mercado Pago, which rejects the
// charge without a full address. Asaas derives it from the stored customer.
type GatewayAddress struct {
	ZipCode      string
	StreetName   string
	StreetNumber string
	Neighborhood string
	City         string
	FederalUnit  string
}

// SplitRecipient is one leg of a split charge. RecipientID is provider-scoped: an
// Asaas walletId today. Exactly one of FixedAmount or Percentage is normally set.
type SplitRecipient struct {
	RecipientID       string
	FixedAmount       float64
	Percentage        float64
	ExternalReference string
	Description       string
}

// ChargeRequest is the provider-agnostic instruction to bill someone.
type ChargeRequest struct {
	Method            Method
	Amount            float64
	DueDate           time.Time
	Description       string
	ExternalReference string
	Customer          GatewayCustomer
	Splits            []SplitRecipient
	// IdempotencyKey is forwarded to providers that support one (Mercado Pago's
	// X-Idempotency-Key). Empty lets the adapter generate a per-request key.
	IdempotencyKey string
}

// Charge is the provider-agnostic view of an issued charge. Payment-method-specific
// artifacts are all optional: a boleto has no PIX payload and vice versa.
type Charge struct {
	ID                string
	Provider          Provider
	Status            Status
	Method            Method
	Amount            float64
	AmountRefunded    float64
	ExternalReference string
	PixQRCodeBase64   string
	PixCopyPaste      string
	BoletoURL         string
	InvoiceURL        string
	DueDate           *time.Time
	// ProviderStatus and ProviderStatusDetail carry the untranslated provider values
	// for logs and support. Never branch business logic on them outside an adapter.
	ProviderStatus       string
	ProviderStatusDetail string
}

// GatewayCapabilities advertises what the wired provider can actually do, so callers
// degrade explicitly instead of silently losing money. Split in particular is not
// portable: Asaas divides one charge across wallet ids, while Mercado Pago requires
// each receiver to be OAuth-onboarded as a marketplace seller and charges through
// that seller's token with an application_fee.
type GatewayCapabilities struct {
	Split            bool
	WalletValidation bool
	PartialRefund    bool
	// BoletoRequiresAddress means the provider refuses a boleto without a full payer
	// address. Mercado Pago does; Asaas derives one from the stored customer. Callers
	// check this to reject up front with an actionable error instead of surfacing an
	// opaque provider rejection to the user.
	BoletoRequiresAddress bool
}

// Complete reports whether the address carries everything a provider that requires one
// will accept. Every field listed is mandatory for a Brazilian boleto, so a partially
// filled address is worth catching before it becomes a provider rejection.
func (a *GatewayAddress) Complete() bool {
	if a == nil {
		return false
	}
	for _, field := range []string{a.ZipCode, a.StreetName, a.StreetNumber, a.Neighborhood, a.City, a.FederalUnit} {
		if strings.TrimSpace(field) == "" {
			return false
		}
	}
	return true
}

// MissingAddressFields names the empty parts of an address, for an error message that
// tells the user what to go and fill in.
func MissingAddressFields(a *GatewayAddress) []string {
	fields := []struct {
		name  string
		value string
	}{
		{"CEP", ""}, {"logradouro", ""}, {"número", ""},
		{"bairro", ""}, {"cidade", ""}, {"estado", ""},
	}
	if a != nil {
		fields[0].value, fields[1].value, fields[2].value = a.ZipCode, a.StreetName, a.StreetNumber
		fields[3].value, fields[4].value, fields[5].value = a.Neighborhood, a.City, a.FederalUnit
	}

	missing := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	return missing
}

// Gateway is the outbound port every payment provider adapter implements. It lives in
// the domain so use cases depend on this interface and never on an infra package.
type Gateway interface {
	Provider() Provider
	Capabilities() GatewayCapabilities

	// CreateCharge issues a charge and returns it fully populated, including the PIX
	// payload when Method is PIX. Providers that expose the QR code through a second
	// call (Asaas) make that call internally.
	CreateCharge(ctx context.Context, req ChargeRequest) (*Charge, error)

	// GetCharge fetches the current state of a charge by its provider-side id.
	GetCharge(ctx context.Context, chargeID string) (*Charge, error)

	// RefundCharge refunds a charge. A non-positive amount means a full refund.
	RefundCharge(ctx context.Context, chargeID string, amount float64, description string) error

	// CancelCharge voids a charge that has not been paid.
	CancelCharge(ctx context.Context, chargeID string) error
}

var (
	// ErrUnknownProvider is returned by ParseProvider for an unrecognized name.
	ErrUnknownProvider = errors.New("unknown payment provider")
	// ErrChargeNotFound means the provider has no charge with that id.
	ErrChargeNotFound = errors.New("charge not found at payment provider")
	// ErrInvalidChargeID guards against issuing a request with an id that would build
	// a malformed or path-traversing URL.
	ErrInvalidChargeID = errors.New("invalid charge id")
	// ErrCustomerDocumentRequired means the payer has no CPF/CNPJ on file. Every
	// supported Brazilian provider refuses to create a charge without one.
	ErrCustomerDocumentRequired = errors.New("customer CPF/CNPJ is required to create a charge")
	// ErrCustomerEmailRequired means the payer has no email. Mercado Pago rejects a
	// payment without payer.email; Asaas does not need it.
	ErrCustomerEmailRequired = errors.New("customer email is required to create a charge")
	// ErrSplitUnsupported means a split was requested from a provider that cannot
	// perform one. Adapters must return this rather than dropping the split, because
	// a silently unsplit charge pays the platform money that belongs to someone else.
	ErrSplitUnsupported = errors.New("payment provider does not support split charges")
	// ErrMethodUnsupported means the provider cannot issue that payment method.
	ErrMethodUnsupported = errors.New("payment provider does not support this billing method")
	// ErrInvalidAmount means the charge amount is not positive.
	ErrInvalidAmount = errors.New("charge amount must be positive")
	// ErrBillingAddressRequired means a boleto was requested without the full payer
	// address the provider demands. Callers should surface it as a 4xx naming the
	// missing fields, not as an opaque provider failure.
	ErrBillingAddressRequired = errors.New("payer address is required for this billing method")
)

// WebhookResolver turns one provider's raw notification into a canonical WebhookEvent.
//
// It exists because providers disagree about how much a notification carries. Asaas
// ships the whole payment, so resolving is a JSON decode. Mercado Pago ships only a
// resource id, so resolving means calling the API. Putting that difference behind a
// port keeps the queue consumer identical for both and free of provider knowledge.
type WebhookResolver interface {
	Provider() Provider

	// Resolve returns the canonical event for a raw notification body.
	//
	// It returns ErrWebhookIgnored when the notification is legitimate but should move
	// no local state (a non-payment topic, or a payment in a state we do not act on),
	// and ErrWebhookMalformed when the payload can never be resolved. Both are
	// terminal: the caller acknowledges and drops. Any other error is treated as
	// transient and the message is retried.
	Resolve(ctx context.Context, raw []byte) (*WebhookEvent, error)
}

var (
	// ErrWebhookIgnored means the notification was understood and deliberately
	// produces no state change. Acknowledge it; retrying would change nothing.
	ErrWebhookIgnored = errors.New("payment webhook carries no actionable state")
	// ErrWebhookMalformed means the payload cannot be resolved by any retry.
	ErrWebhookMalformed = errors.New("payment webhook payload is malformed")
)
