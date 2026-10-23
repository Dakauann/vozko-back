package invoice_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/affiliate"
	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/domain/user"
)

type renewInvoicePixUseCase struct {
	invoices invoice.Repository
	renewals invoice.ChargeRenewals
	gateway  payment.Gateway
	charges  *chargeComposer
	alerts   billing.OpsAlerter
	now      func() time.Time
}

func NewRenewInvoicePixUseCase(
	invoices invoice.Repository,
	renewals invoice.ChargeRenewals,
	users user.UserRepository,
	gateway payment.Gateway,
	affiliates affiliate.Repository,
	alerts billing.OpsAlerter,
) invoice.RenewPixUseCase {
	return newRenewInvoicePix(invoices, renewals, users, gateway, affiliates, alerts, time.Now)
}

func newRenewInvoicePix(
	invoices invoice.Repository,
	renewals invoice.ChargeRenewals,
	users user.UserRepository,
	gateway payment.Gateway,
	affiliates affiliate.Repository,
	alerts billing.OpsAlerter,
	now func() time.Time,
) *renewInvoicePixUseCase {
	return &renewInvoicePixUseCase{
		invoices: invoices,
		renewals: renewals,
		gateway:  gateway,
		charges:  &chargeComposer{userRepo: users, gateway: gateway, affiliateRepo: affiliates},
		alerts:   alerts,
		now:      now,
	}
}

func (uc *renewInvoicePixUseCase) Execute(ctx context.Context, input invoice.RenewPixInput) (*invoice.Invoice, error) {
	inv, err := uc.load(input)
	if err != nil {
		return nil, err
	}
	now := uc.now()
	deadline, err := inv.RenewalDeadline(now)
	if errors.Is(err, invoice.ErrChargeStillLive) {
		return inv, nil
	}
	if err != nil {
		return nil, err
	}

	provider := uc.gateway.Provider()
	if inv.Provider != "" && inv.Provider != provider {
		log.Printf("[invoice-pix] invoice %s was charged on %s but the active provider is %s; refusing to renew", inv.ID, inv.Provider, provider)
		return nil, invoice.ErrPixUnavailable
	}

	request, err := uc.request(inv, deadline)
	if err != nil {
		return nil, err
	}

	previous := invoice.SupersededCharge{Provider: provider, ExternalID: inv.ExternalID}
	acquired, err := uc.renewals.BeginRenewal(inv.ID, previous, now.Add(invoice.ChargeRenewalLease), now)
	if err != nil {
		return nil, fmt.Errorf("begin renewal of invoice %s: %w", inv.ID, err)
	}
	if !acquired {
		return uc.afterLostRace(input)
	}

	if err := uc.retire(ctx, inv.ID, previous.ExternalID); err != nil {
		uc.abort(inv.ID, previous, true)
		return nil, err
	}

	charge, err := uc.gateway.CreateCharge(ctx, request)
	if err != nil {
		uc.abort(inv.ID, previous, false)
		log.Printf("[invoice-pix] new charge for invoice %s failed: %v", inv.ID, err)
		return nil, fmt.Errorf("%w: %v", invoice.ErrPixUnavailable, err)
	}

	attached, err := uc.renewals.CompleteRenewal(inv.ID, previous.ExternalID, chargeAttachment(charge, request.ExpiresAt))
	if err != nil || !attached {
		return uc.afterLostCompletion(ctx, input, previous, charge, err)
	}
	return uc.load(input)
}

func (uc *renewInvoicePixUseCase) load(input invoice.RenewPixInput) (*invoice.Invoice, error) {
	inv, err := uc.invoices.GetByID(input.InvoiceID)
	if err != nil {
		return nil, err
	}
	if inv == nil || inv.WorkspaceID != input.WorkspaceID {
		return nil, invoice.ErrInvoiceNotFound
	}
	return inv, nil
}

func (uc *renewInvoicePixUseCase) request(inv *invoice.Invoice, deadline time.Time) (payment.ChargeRequest, error) {
	customer, err := uc.charges.customer(inv.UserID, payment.MethodPix)
	if err != nil {
		return payment.ChargeRequest{}, err
	}
	return payment.ChargeRequest{
		Method:            payment.MethodPix,
		Amount:            inv.AmountBRL,
		DueDate:           deadline,
		ExpiresAt:         deadline,
		Description:       chargeDescription(inv.Purpose, inv.Description),
		ExternalReference: invoice.ExternalReference(inv.ID),
		Customer:          customer,
		Splits:            uc.charges.splits(inv.WorkspaceID),
		IdempotencyKey:    invoice.RenewalIdempotencyKey(inv.ID, inv.ExternalID),
	}, nil
}

func (uc *renewInvoicePixUseCase) retire(ctx context.Context, invoiceID, chargeID string) error {
	cancelErr := uc.gateway.CancelCharge(ctx, chargeID)
	if cancelErr == nil || errors.Is(cancelErr, payment.ErrChargeNotFound) {
		return nil
	}
	charge, err := uc.gateway.GetCharge(ctx, chargeID)
	if errors.Is(err, payment.ErrChargeNotFound) {
		return nil
	}
	if err != nil {
		log.Printf("[invoice-pix] invoice %s: cancel of charge %s failed (%v) and it could not be read back: %v", invoiceID, chargeID, cancelErr, err)
		return fmt.Errorf("%w: %v", invoice.ErrPixUnavailable, cancelErr)
	}
	switch charge.Status {
	case payment.StatusCancelled:
		return nil
	case payment.StatusReceived, payment.StatusReceivedInCash:
		return invoice.ErrAlreadyPaid
	default:
		log.Printf("[invoice-pix] invoice %s: charge %s is %s at the provider and could not be cancelled: %v", invoiceID, chargeID, charge.Status, cancelErr)
		return fmt.Errorf("%w: %v", invoice.ErrPixUnavailable, cancelErr)
	}
}

func (uc *renewInvoicePixUseCase) abort(invoiceID string, previous invoice.SupersededCharge, previousStillPayable bool) {
	if err := uc.renewals.AbortRenewal(invoiceID, previous, previousStillPayable); err != nil {
		log.Printf("[invoice-pix] invoice %s: could not release the renewal of charge %s, it expires with its lease: %v", invoiceID, previous.ExternalID, err)
	}
}

func (uc *renewInvoicePixUseCase) afterLostRace(input invoice.RenewPixInput) (*invoice.Invoice, error) {
	current, err := uc.load(input)
	if err != nil {
		return nil, err
	}
	switch current.PaymentAt(uc.now()).State {
	case invoice.PaymentOpen:
		return current, nil
	case invoice.PaymentRenewable:
		return nil, invoice.ErrPixRenewInProgress
	default:
		return nil, closedError(current)
	}
}

func (uc *renewInvoicePixUseCase) afterLostCompletion(ctx context.Context, input invoice.RenewPixInput, previous invoice.SupersededCharge, charge *payment.Charge, completeErr error) (*invoice.Invoice, error) {
	current, err := uc.load(input)
	if err != nil {
		uc.alert("PIX renewal could not be confirmed",
			fmt.Sprintf("invoice=%s charge=%s provider=%s: check whether the invoice carries this charge and cancel it at the provider if not (attach: %v, reload: %v)",
				input.InvoiceID, charge.ID, uc.gateway.Provider(), completeErr, err))
		return nil, fmt.Errorf("%w: renewal of invoice %s could not be confirmed: %v", invoice.ErrPixUnavailable, input.InvoiceID, err)
	}
	if current.ExternalID == charge.ID {
		return current, nil
	}
	uc.abort(input.InvoiceID, previous, false)
	if cancelErr := uc.gateway.CancelCharge(ctx, charge.ID); cancelErr != nil {
		uc.alert("PIX charge left open after a lost renewal",
			fmt.Sprintf("invoice=%s charge=%s provider=%s: cancel it at the provider: %v", input.InvoiceID, charge.ID, uc.gateway.Provider(), cancelErr))
	}
	if completeErr != nil {
		return nil, fmt.Errorf("%w: attach charge %s to invoice %s: %v", invoice.ErrPixUnavailable, charge.ID, input.InvoiceID, completeErr)
	}
	if current.PaymentAt(uc.now()).State == invoice.PaymentOpen {
		return current, nil
	}
	return nil, closedError(current)
}

func closedError(inv *invoice.Invoice) error {
	if inv.Status == invoice.StatusPaid {
		return invoice.ErrAlreadyPaid
	}
	if inv.AwaitingPayment() {
		return invoice.ErrPixUnavailable
	}
	return invoice.ErrNotPayable
}

func (uc *renewInvoicePixUseCase) alert(subject, detail string) {
	if err := uc.alerts.Alert(context.Background(), subject, detail); err != nil {
		log.Printf("[invoice-pix] ALERT %s: %s (alerter failed: %v)", subject, detail, err)
	}
}
