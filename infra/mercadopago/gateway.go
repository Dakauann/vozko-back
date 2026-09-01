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

// gatewayAdapter adapts the Mercado Pago client to the provider-agnostic
// payment.Gateway port. Every Mercado-Pago-shaped concern lives here: the payer
// identification model, the inline PIX payload, the ISO-8601 expiration layout, and
// the fact that splits are not expressible.
type gatewayAdapter struct {
	client             Client
	now                func() time.Time
	sandboxPayerEmail  string
	sandboxPayerStatus string
}

// GatewayOption configures the adapter.
type GatewayOption func(*gatewayAdapter)

// WithClock overrides the clock used for expiration clamping. Intended for tests.
func WithClock(now func() time.Time) GatewayOption {
	return func(g *gatewayAdapter) {
		if now != nil {
			g.now = now
		}
	}
}

// WithSandboxPayerEmail addresses every charge to a fixed payer instead of the real
// customer.
//
// Mercado Pago's sandbox rejects a charge whose payer is not one of its own test users,
// so without this there is no way to drive the real billing flow end to end against
// sandbox credentials. The caller is responsible for only supplying it outside
// production — config.LoadConfig forces it empty unless APP_ENV=development — and every
// substitution is logged, because a charge addressed to someone other than the customer
// must never be invisible.
func WithSandboxPayerEmail(email string) GatewayOption {
	return func(g *gatewayAdapter) { g.sandboxPayerEmail = strings.TrimSpace(email) }
}

// Sandbox status keywords. Mercado Pago reads them from payer.first_name and forces the
// resulting payment into that state, which is the only way to exercise a PIX payment in
// sandbox: test QR codes are not payable by real bank apps.
//
// Reference: https://www.mercadopago.com.br/developers/pt/docs/checkout-api-orders/integration-test/pix
const (
	SandboxStatusApproved = "APRO"
	SandboxStatusPending  = "CONT"
	SandboxStatusRejected = "OTHE"
)

// WithSandboxPayerStatus forces every sandbox charge into a chosen final state by
// sending a Mercado Pago test keyword as payer.first_name.
//
// This is what makes the webhook path testable: without it a sandbox PIX charge sits in
// "pending" forever, no notification is ever emitted, and the credit-balance flow can
// only be exercised by hand. As with the payer override it is development-only and
// additionally refused for non-TEST tokens at wiring time, because in production it
// would replace a real customer's name on a real charge.
func WithSandboxPayerStatus(status string) GatewayOption {
	return func(g *gatewayAdapter) { g.sandboxPayerStatus = strings.ToUpper(strings.TrimSpace(status)) }
}

// NewGateway wraps a Mercado Pago client as a payment.Gateway.
func NewGateway(c Client, opts ...GatewayOption) payment.Gateway {
	g := &gatewayAdapter{client: c, now: time.Now}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *gatewayAdapter) Provider() payment.Provider { return payment.ProviderMercadoPago }

// Capabilities reports no split support, and that is a real product limitation rather
// than an unfinished implementation.
//
// Asaas divides a single charge across walletIds supplied per request. Mercado Pago has
// no equivalent: its marketplace split requires every receiver to complete an OAuth
// authorization, after which the charge is created with THAT SELLER's access token and
// the platform's cut is taken as application_fee. That is a different onboarding model
// and a different money flow, not a different field name, and it cannot be synthesized
// from a walletId this system already stores.
//
// CreateCharge therefore rejects a split request outright instead of quietly issuing an
// unsplit charge, which would route someone else's commission into the platform account.
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
	// The override is applied after the emptiness check on purpose: a missing customer
	// email is a real data problem that must still surface in development, rather than
	// being papered over by the sandbox payer.
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

	// Boleto is rejected without a full payer address, so refuse before spending a
	// round trip and tell the caller exactly what is missing.
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
	// Mercado Pago's refund endpoint takes no description; the reason is set by the
	// platform. The argument is accepted to keep the port uniform and is logged by
	// callers rather than dropped silently here.
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

	// The customer-facing link differs by method: PIX gets the hosted QR page, boleto
	// gets the printable slip.
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

// ToWebhookEvent converts a fetched payment into the canonical webhook event the
// shared handler consumes. It returns false when the payment's state should move
// nothing locally, so the caller acknowledges the notification and stops.
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
