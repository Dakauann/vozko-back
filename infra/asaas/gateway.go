package asaas

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/payment"
)

type gatewayAdapter struct {
	svc AsaasServiceUseCases
}

func NewGateway(svc AsaasServiceUseCases) payment.Gateway {
	return &gatewayAdapter{svc: svc}
}

func (g *gatewayAdapter) Provider() payment.Provider { return payment.ProviderAsaas }

func (g *gatewayAdapter) Capabilities() payment.GatewayCapabilities {
	return payment.GatewayCapabilities{
		Split:                 true,
		WalletValidation:      true,
		PartialRefund:         true,
		BoletoRequiresAddress: false,
	}
}

func asaasBillingType(m payment.Method) string {
	switch m {
	case payment.MethodBoleto:
		return "BOLETO"
	case payment.MethodCreditCard:
		return "CREDIT_CARD"
	default:
		return "PIX"
	}
}

func methodFromAsaas(raw string) payment.Method {
	return payment.NormalizeMethod(raw)
}

func (g *gatewayAdapter) CreateCharge(ctx context.Context, req payment.ChargeRequest) (*payment.Charge, error) {
	if g == nil || g.svc == nil {
		return nil, fmt.Errorf("asaas gateway: service not configured")
	}
	if req.Amount <= 0 {
		return nil, payment.ErrInvalidAmount
	}
	if strings.TrimSpace(req.Customer.Document) == "" {
		return nil, payment.ErrCustomerDocumentRequired
	}

	dueDate := req.DueDate
	if dueDate.IsZero() {
		dueDate = time.Now().AddDate(0, 0, 3)
	}

	method := req.Method
	if method == "" {
		method = payment.MethodPix
	}

	charge := &AsaasPayment{
		BillingType:       asaasBillingType(method),
		Value:             req.Amount,
		DueDate:           dueDate.Format("2006-01-02"),
		Description:       req.Description,
		ExternalReference: req.ExternalReference,
	}
	for _, s := range req.Splits {
		charge.Split = append(charge.Split, AsaasSplit{
			WalletID:          s.RecipientID,
			FixedValue:        s.FixedAmount,
			PercentualValue:   s.Percentage,
			ExternalReference: s.ExternalReference,
			Description:       s.Description,
		})
	}

	created, err := g.svc.CreatePayment(req.Customer.Name, req.Customer.Document, charge)
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, fmt.Errorf("asaas gateway: empty create payment response")
	}

	out := g.toCharge(created, method)

	if method == payment.MethodPix && created.ID != "" {
		qr, copyPaste, qrErr := g.svc.GetPaymentQrCode(created.ID)
		if qrErr != nil {
			log.Printf("[asaas-gateway] charge %s created but PIX QR code fetch failed: %v", created.ID, qrErr)
		} else {
			out.PixQRCodeBase64 = qr
			out.PixCopyPaste = copyPaste
		}
	}

	return out, nil
}

func (g *gatewayAdapter) GetCharge(ctx context.Context, chargeID string) (*payment.Charge, error) {
	if g == nil || g.svc == nil {
		return nil, fmt.Errorf("asaas gateway: service not configured")
	}
	if err := validateAsaasID(chargeID, "charge id"); err != nil {
		return nil, fmt.Errorf("%w: %v", payment.ErrInvalidChargeID, err)
	}
	got, err := g.svc.GetPayment(chargeID)
	if err != nil {
		return nil, err
	}
	if got == nil {
		return nil, payment.ErrChargeNotFound
	}
	return g.toCharge(got, methodFromAsaas(got.BillingType)), nil
}

func (g *gatewayAdapter) RefundCharge(ctx context.Context, chargeID string, amount float64, description string) error {
	if g == nil || g.svc == nil {
		return fmt.Errorf("asaas gateway: service not configured")
	}
	if amount < 0 {
		amount = 0
	}
	return g.svc.RefundPayment(chargeID, int64(amount), description)
}

func (g *gatewayAdapter) CancelCharge(ctx context.Context, chargeID string) error {
	if g == nil || g.svc == nil {
		return fmt.Errorf("asaas gateway: service not configured")
	}
	return g.svc.DeletePayment(chargeID)
}

func (g *gatewayAdapter) toCharge(p *AsaasPayment, method payment.Method) *payment.Charge {
	out := &payment.Charge{
		ID:                p.ID,
		Provider:          payment.ProviderAsaas,
		Method:            method,
		Amount:            p.Value,
		ExternalReference: p.ExternalReference,
		BoletoURL:         p.BankSlipUrl,
		InvoiceURL:        p.InvoiceUrl,
		Status:            payment.StatusPending,
	}
	if due, err := time.Parse("2006-01-02", p.DueDate); err == nil {
		out.DueDate = &due
	}
	return out
}

var _ payment.Gateway = (*gatewayAdapter)(nil)
