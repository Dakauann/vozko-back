package mercadopago

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/payment"
)

type gatewayAdapter struct {
	client             Client
	now                func() time.Time
	sandboxPayerEmail  string
	sandboxPayerStatus string
}

type GatewayOption func(*gatewayAdapter)

func WithClock(now func() time.Time) GatewayOption {
	return func(g *gatewayAdapter) {
		if now != nil {
			g.now = now
		}
	}
}

func WithSandboxPayerEmail(email string) GatewayOption {
	return func(g *gatewayAdapter) { g.sandboxPayerEmail = strings.TrimSpace(email) }
}

const (
	SandboxStatusApproved = "APRO"
	SandboxStatusPending  = "CONT"
	SandboxStatusRejected = "OTHE"
)

func WithSandboxPayerStatus(status string) GatewayOption {
	return func(g *gatewayAdapter) { g.sandboxPayerStatus = strings.ToUpper(strings.TrimSpace(status)) }
}

func NewGateway(c Client, opts ...GatewayOption) payment.Gateway {
	g := &gatewayAdapter{client: c, now: time.Now}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *gatewayAdapter) Provider() payment.Provider { return payment.ProviderMercadoPago }

func (g *gatewayAdapter) Capabilities() payment.GatewayCapabilities {
	return payment.GatewayCapabilities{
		Split:                 false,
		WalletValidation:      false,
		PartialRefund:         true,
		BoletoRequiresAddress: true,
	}
}

func (g *gatewayAdapter) CreateCharge(ctx context.Context, req payment.ChargeRequest) (*payment.Charge, error) {
	if g == nil || g.client == nil {
		return nil, errors.New("mercadopago gateway: client not configured")
	}
	if req.Amount <= 0 {
		return nil, payment.ErrInvalidAmount
	}
	if len(req.Splits) > 0 {
		return nil, fmt.Errorf("%w: %d recipient(s) requested", payment.ErrSplitUnsupported, len(req.Splits))
	}

	document := OnlyDigits(req.Customer.Document)
	if document == "" {
		return nil, payment.ErrCustomerDocumentRequired
	}
	email := strings.TrimSpace(req.Customer.Email)
	if email == "" {
		return nil, payment.ErrCustomerEmailRequired
	}
	if g.sandboxPayerEmail != "" && !strings.EqualFold(email, g.sandboxPayerEmail) {
		log.Printf("[mercadopago-gateway] SANDBOX: charge %q addressed to test payer %s instead of %s",
			req.ExternalReference, g.sandboxPayerEmail, email)
		email = g.sandboxPayerEmail
	}

	method := req.Method
	if method == "" {
		method = payment.MethodPix
	}
	methodID, err := PaymentMethodIDFor(method)
	if err != nil {
		return nil, err
	}

	first, last := SplitName(req.Customer.Name)
	if g.sandboxPayerStatus != "" {
		log.Printf("[mercadopago-gateway] SANDBOX: charge %q forced to status keyword %s (payer.first_name)",
			req.ExternalReference, g.sandboxPayerStatus)
		first = g.sandboxPayerStatus
	}
	apiReq := CreatePaymentRequest{
		TransactionAmount: req.Amount,
		PaymentMethodID:   methodID,
		Description:       req.Description,
		ExternalReference: req.ExternalReference,
		Payer: PayerRequest{
			Email:     email,
			FirstName: first,
			LastName:  last,
			Identification: &Identification{
				Type:   IdentificationTypeFor(document),
				Number: document,
			},
		},
	}

	if method == payment.MethodBoleto {
		addr := req.Customer.Address
		if !addr.Complete() {
			return nil, fmt.Errorf("%w: boleto is missing %s",
				payment.ErrBillingAddressRequired, strings.Join(payment.MissingAddressFields(addr), ", "))
		}
		apiReq.Payer.Address = &PayerAddress{
			ZipCode:      strings.TrimSpace(addr.ZipCode),
			StreetName:   strings.TrimSpace(addr.StreetName),
			StreetNumber: strings.TrimSpace(addr.StreetNumber),
			Neighborhood: strings.TrimSpace(addr.Neighborhood),
			City:         strings.TrimSpace(addr.City),
			FederalUnit:  strings.TrimSpace(addr.FederalUnit),
		}
	}

	if !req.DueDate.IsZero() {
		apiReq.DateOfExpiration = FormatExpiration(req.DueDate, g.now(), method == payment.MethodPix)
	}

	created, err := g.client.CreatePayment(ctx, apiReq, req.IdempotencyKey)
	if err != nil {
		return nil, err
	}

	return g.toCharge(created), nil
}

func (g *gatewayAdapter) GetCharge(ctx context.Context, chargeID string) (*payment.Charge, error) {
	if g == nil || g.client == nil {
		return nil, errors.New("mercadopago gateway: client not configured")
	}
	got, err := g.client.GetPayment(ctx, chargeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, payment.ErrChargeNotFound
		}
		if errors.Is(err, ErrInvalidPaymentID) {
			return nil, fmt.Errorf("%w: %v", payment.ErrInvalidChargeID, err)
		}
		return nil, err
	}
	return g.toCharge(got), nil
}

func (g *gatewayAdapter) RefundCharge(ctx context.Context, chargeID string, amount float64, description string) error {
	if g == nil || g.client == nil {
		return errors.New("mercadopago gateway: client not configured")
	}
	if _, err := g.client.RefundPayment(ctx, chargeID, amount, ""); err != nil {
		if errors.Is(err, ErrNotFound) {
			return payment.ErrChargeNotFound
		}
		return err
	}
	return nil
}

func (g *gatewayAdapter) CancelCharge(ctx context.Context, chargeID string) error {
	if g == nil || g.client == nil {
		return errors.New("mercadopago gateway: client not configured")
	}
	if _, err := g.client.CancelPayment(ctx, chargeID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return payment.ErrChargeNotFound
		}
		return err
	}
	return nil
}

func (g *gatewayAdapter) toCharge(p *Payment) *payment.Charge {
	if p == nil {
		return nil
	}
	out := &payment.Charge{
		ID:                   FormatPaymentID(p.ID),
		Provider:             payment.ProviderMercadoPago,
		Status:               MapStatus(p.Status),
		Method:               MapMethod(p.PaymentMethodID),
		Amount:               p.TransactionAmount,
		AmountRefunded:       p.TransactionAmountRefunded,
		ExternalReference:    p.ExternalReference,
		PixQRCodeBase64:      p.PointOfInteraction.TransactionData.QRCodeBase64,
		PixCopyPaste:         p.PointOfInteraction.TransactionData.QRCode,
		ProviderStatus:       p.Status,
		ProviderStatusDetail: p.StatusDetail,
	}

	if url := strings.TrimSpace(p.TransactionDetails.ExternalResourceURL); url != "" {
		out.BoletoURL = url
		out.InvoiceURL = url
	}
	if ticket := strings.TrimSpace(p.PointOfInteraction.TransactionData.TicketURL); ticket != "" {
		out.InvoiceURL = ticket
	}
	if p.DateOfExpiration != nil && !p.DateOfExpiration.IsZero() {
		due := *p.DateOfExpiration
		out.DueDate = &due
	}
	return out
}

func ToWebhookEvent(notificationID string, p *Payment) (*payment.WebhookEvent, bool) {
	if p == nil {
		return nil, false
	}
	event := MapEvent(p)
	if event == "" {
		return nil, false
	}
	return &payment.WebhookEvent{
		ID:       notificationID,
		Event:    event,
		Provider: payment.ProviderMercadoPago,
		Payment: payment.WebhookPayment{
			ID:                FormatPaymentID(p.ID),
			ExternalReference: p.ExternalReference,
			BillingType:       string(MapMethod(p.PaymentMethodID)),
			Value:             p.TransactionAmount,
			Status:            p.Status,
		},
	}, true
}

var _ payment.Gateway = (*gatewayAdapter)(nil)
