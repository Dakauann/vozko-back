package mercadopago

import "time"

const (
	PaymentMethodPix    = "pix"
	PaymentMethodBoleto = "bolbradesco"
)

const (
	IdentificationCPF  = "CPF"
	IdentificationCNPJ = "CNPJ"
)

type CreatePaymentRequest struct {
	TransactionAmount   float64        `json:"transaction_amount"`
	PaymentMethodID     string         `json:"payment_method_id"`
	Description         string         `json:"description,omitempty"`
	ExternalReference   string         `json:"external_reference,omitempty"`
	NotificationURL     string         `json:"notification_url,omitempty"`
	DateOfExpiration    string         `json:"date_of_expiration,omitempty"`
	ApplicationFee      float64        `json:"application_fee,omitempty"`
	StatementDescriptor string         `json:"statement_descriptor,omitempty"`
	Installments        int            `json:"installments,omitempty"`
	Payer               PayerRequest   `json:"payer"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

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

type PointOfInteraction struct {
	Type            string          `json:"type"`
	TransactionData TransactionData `json:"transaction_data"`
}

type TransactionData struct {
	QRCode       string `json:"qr_code"`
	QRCodeBase64 string `json:"qr_code_base64"`
	TicketURL    string `json:"ticket_url"`
}

type TransactionDetails struct {
	ExternalResourceURL string  `json:"external_resource_url"`
	DigitableLine       string  `json:"digitable_line"`
	NetReceivedAmount   float64 `json:"net_received_amount"`
	TotalPaidAmount     float64 `json:"total_paid_amount"`
}

type Refund struct {
	ID        int64   `json:"id"`
	PaymentID int64   `json:"payment_id"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
	Reason    string  `json:"reason"`
}

type createRefundRequest struct {
	Amount float64 `json:"amount,omitempty"`
}

type updatePaymentRequest struct {
	Status string `json:"status,omitempty"`
}

type APIError struct {
	Message string       `json:"message"`
	Error   string       `json:"error"`
	Status  int          `json:"status"`
	Cause   []ErrorCause `json:"cause"`
}

type ErrorCause struct {
	Code        any    `json:"code,omitempty"`
	Description string `json:"description"`
}
