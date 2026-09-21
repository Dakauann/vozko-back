package payment

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Provider string

const (
	ProviderAsaas       Provider = "asaas"
	ProviderMercadoPago Provider = "mercadopago"
)

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

type Method string

const (
	MethodPix        Method = "PIX"
	MethodBoleto     Method = "BOLETO"
	MethodCreditCard Method = "CREDIT_CARD"
)

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

type GatewayCustomer struct {
	Name     string
	Email    string
	Document string
	Address  *GatewayAddress
}

type GatewayAddress struct {
	ZipCode      string
	StreetName   string
	StreetNumber string
	Neighborhood string
	City         string
	FederalUnit  string
}

type SplitRecipient struct {
	RecipientID       string
	FixedAmount       float64
	Percentage        float64
	ExternalReference string
	Description       string
}

type ChargeRequest struct {
	Method            Method
	Amount            float64
	DueDate           time.Time
	Description       string
	ExternalReference string
	Customer          GatewayCustomer
	Splits            []SplitRecipient
	IdempotencyKey    string
}

type Charge struct {
	ID                   string
	Provider             Provider
	Status               Status
	Method               Method
	Amount               float64
	AmountRefunded       float64
	ExternalReference    string
	PixQRCodeBase64      string
	PixCopyPaste         string
	BoletoURL            string
	InvoiceURL           string
	DueDate              *time.Time
	ProviderStatus       string
	ProviderStatusDetail string
}

type GatewayCapabilities struct {
	Split                 bool
	WalletValidation      bool
	PartialRefund         bool
	BoletoRequiresAddress bool
}

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

type Gateway interface {
	Provider() Provider
	Capabilities() GatewayCapabilities

	CreateCharge(ctx context.Context, req ChargeRequest) (*Charge, error)

	GetCharge(ctx context.Context, chargeID string) (*Charge, error)

	RefundCharge(ctx context.Context, chargeID string, amount float64, description string) error

	CancelCharge(ctx context.Context, chargeID string) error
}

var (
	ErrUnknownProvider          = errors.New("unknown payment provider")
	ErrChargeNotFound           = errors.New("charge not found at payment provider")
	ErrInvalidChargeID          = errors.New("invalid charge id")
	ErrCustomerDocumentRequired = errors.New("customer CPF/CNPJ is required to create a charge")
	ErrCustomerEmailRequired    = errors.New("customer email is required to create a charge")
	ErrSplitUnsupported         = errors.New("payment provider does not support split charges")
	ErrMethodUnsupported        = errors.New("payment provider does not support this billing method")
	ErrInvalidAmount            = errors.New("charge amount must be positive")
	ErrBillingAddressRequired   = errors.New("payer address is required for this billing method")
)

type WebhookResolver interface {
	Provider() Provider

	Resolve(ctx context.Context, raw []byte) (*WebhookEvent, error)
}

var (
	ErrWebhookIgnored   = errors.New("payment webhook carries no actionable state")
	ErrWebhookMalformed = errors.New("payment webhook payload is malformed")
)
