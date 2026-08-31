package mercadopago

import "time"

// Payment method identifiers accepted by POST /v1/payments in Brazil. Unlike Asaas,
// which names the family ("PIX"/"BOLETO"), Mercado Pago names the concrete instrument,
// so boleto is "bolbradesco".
const (
	PaymentMethodPix    = "pix"
	PaymentMethodBoleto = "bolbradesco"
)

// Identification document types recognized by Mercado Pago Brazil.
const (
	IdentificationCPF  = "CPF"
	IdentificationCNPJ = "CNPJ"
)

// CreatePaymentRequest is the body of POST /v1/payments.
//
// Reference: https://www.mercadopago.com/developers/en/reference/payments/_payments/post
type CreatePaymentRequest struct {
	TransactionAmount float64 `json:"transaction_amount"`
	PaymentMethodID   string  `json:"payment_method_id"`
	Description       string  `json:"description,omitempty"`
	ExternalReference string  `json:"external_reference,omitempty"`
	// NotificationURL overrides the account-level webhook URL for this payment only.
	// It is what makes the x-signature data.id query parameter arrive on our endpoint.
	NotificationURL string `json:"notification_url,omitempty"`
	// DateOfExpiration is ISO-8601 with milliseconds and an explicit offset, e.g.
	// "2026-09-03T23:59:59.000-03:00". Mercado Pago rejects other layouts.
	DateOfExpiration string `json:"date_of_expiration,omitempty"`
	// ApplicationFee is the marketplace commission, valid only when the request is
	// authenticated with a seller's OAuth token. It is not a general split mechanism.
	ApplicationFee      float64        `json:"application_fee,omitempty"`
	StatementDescriptor string         `json:"statement_descriptor,omitempty"`
	Installments        int            `json:"installments,omitempty"`
	Payer               PayerRequest   `json:"payer"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

// PayerRequest identifies the person being charged. Email is always required;
// identification is required for PIX and boleto in Brazil, and the address is
// additionally required for boleto.
type PayerRequest struct {
	Email          string          `json:"email"`
	FirstName      string          `json:"first_name,omitempty"`
	LastName       string          `json:"last_name,omitempty"`
	Identification *Identification `json:"identification,omitempty"`
	Address        *PayerAddress   `json:"address,omitempty"`
}

type Identification struct {
	Type   string `json:"type,omitempty"`
	Number string `json:"number,omitempty"`
}

type PayerAddress struct {
	ZipCode      string `json:"zip_code,omitempty"`
	StreetName   string `json:"street_name,omitempty"`
	StreetNumber string `json:"street_number,omitempty"`
	Neighborhood string `json:"neighborhood,omitempty"`
	City         string `json:"city,omitempty"`
	FederalUnit  string `json:"federal_unit,omitempty"`
}

// Payment is the payment resource returned by POST/GET /v1/payments. Only the fields
// this integration reads are modelled; Mercado Pago returns many more.
type Payment struct {
	ID                        int64      `json:"id"`
	Status                    string     `json:"status"`
	StatusDetail              string     `json:"status_detail"`
	ExternalReference         string     `json:"external_reference"`
	Description               string     `json:"description"`
	PaymentMethodID           string     `json:"payment_method_id"`
	PaymentTypeID             string     `json:"payment_type_id"`
	CurrencyID                string     `json:"currency_id"`
	TransactionAmount         float64    `json:"transaction_amount"`
	TransactionAmountRefunded float64    `json:"transaction_amount_refunded"`
	LiveMode                  bool       `json:"live_mode"`
	DateCreated               *time.Time `json:"date_created"`
	DateApproved              *time.Time `json:"date_approved"`
	DateOfExpiration          *time.Time `json:"date_of_expiration"`

	Payer              PaymentPayer       `json:"payer"`
	PointOfInteraction PointOfInteraction `json:"point_of_interaction"`
	TransactionDetails TransactionDetails `json:"transaction_details"`
	Refunds            []Refund           `json:"refunds"`
}

type PaymentPayer struct {
	ID             string         `json:"id"`
	Email          string         `json:"email"`
	FirstName      string         `json:"first_name"`
	LastName       string         `json:"last_name"`
	Identification Identification `json:"identification"`
}

// PointOfInteraction carries the PIX artifacts. Mercado Pago returns them inline on
// creation, so no second call is needed (Asaas requires one).
type PointOfInteraction struct {
	Type            string          `json:"type"`
	TransactionData TransactionData `json:"transaction_data"`
}

type TransactionData struct {
	QRCode       string `json:"qr_code"`
	QRCodeBase64 string `json:"qr_code_base64"`
	TicketURL    string `json:"ticket_url"`
}

// TransactionDetails carries the boleto artifacts; ExternalResourceURL is the
// printable slip.
type TransactionDetails struct {
	ExternalResourceURL string  `json:"external_resource_url"`
	DigitableLine       string  `json:"digitable_line"`
	NetReceivedAmount   float64 `json:"net_received_amount"`
	TotalPaidAmount     float64 `json:"total_paid_amount"`
}

// Refund is one refund of a payment, returned by POST /v1/payments/{id}/refunds and
// embedded in the payment resource.
type Refund struct {
	ID        int64   `json:"id"`
	PaymentID int64   `json:"payment_id"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
	Reason    string  `json:"reason"`
}

// createRefundRequest is the partial-refund body. A full refund sends an empty body.
type createRefundRequest struct {
	Amount float64 `json:"amount,omitempty"`
}

// updatePaymentRequest is the body of PUT /v1/payments/{id}, used here only to cancel.
type updatePaymentRequest struct {
	Status string `json:"status,omitempty"`
}

// APIError is Mercado Pago's error envelope.
type APIError struct {
	Message string       `json:"message"`
	Error   string       `json:"error"`
	Status  int          `json:"status"`
	Cause   []ErrorCause `json:"cause"`
}

// ErrorCause entries carry a machine code. Mercado Pago is inconsistent about whether
// code is a number or a string, so it is decoded as a raw JSON scalar.
type ErrorCause struct {
	Code        any    `json:"code,omitempty"`
	Description string `json:"description"`
}
