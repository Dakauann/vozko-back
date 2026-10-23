package invoice_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/address"
	"vozko/domain/affiliate"
	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/domain/user"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"

	"github.com/google/uuid"
)

type createInvoiceUseCase struct {
	invoiceRepo         invoice.Repository
	gateway             payment.Gateway
	charges             *chargeComposer
	exchangeRateRepo    workspace_pricing.Repository
	currentSubscription workspace_plan.EnsureCurrentWorkspaceSubscriptionUseCase
	affiliateRepo       affiliate.Repository
	trackReferral       affiliate.TrackReferralUseCase
}

func NewCreateInvoiceUseCase(
	invoiceRepo invoice.Repository,
	userRepo user.UserRepository,
	addressRepo address.AddressRepository,
	gateway payment.Gateway,
	exchangeRateRepo workspace_pricing.Repository,
	currentSubscription workspace_plan.EnsureCurrentWorkspaceSubscriptionUseCase,
	affiliateRepo affiliate.Repository,
	trackReferral affiliate.TrackReferralUseCase,
) invoice.CreateInvoiceUseCase {
	return &createInvoiceUseCase{
		invoiceRepo: invoiceRepo,
		gateway:     gateway,
		charges: &chargeComposer{
			userRepo:      userRepo,
			addressRepo:   addressRepo,
			gateway:       gateway,
			affiliateRepo: affiliateRepo,
		},
		exchangeRateRepo:    exchangeRateRepo,
		currentSubscription: currentSubscription,
		affiliateRepo:       affiliateRepo,
		trackReferral:       trackReferral,
	}
}

func (uc *createInvoiceUseCase) Execute(input invoice.CreateInvoiceInput) (*invoice.CreateInvoiceOutput, error) {
	if input.AmountBRL <= 0 {
		return nil, invoice.ErrInvalidAmount
	}

	purpose := input.Purpose.Normalize()
	if !purpose.Valid() {
		return nil, invoice.ErrInvalidPurpose
	}

	planDefinitionID := strings.TrimSpace(input.PlanDefinitionID)
	switch purpose {
	case invoice.PurposeTopUp:
		if err := uc.ensureRechargeAllowed(input.WorkspaceID); err != nil {
			return nil, err
		}
	case invoice.PurposeSubscription:
		if planDefinitionID == "" {
			return nil, invoice.ErrPlanDefinitionRequired
		}
	}

	billingType := input.BillingType
	if billingType == "" {
		billingType = "PIX"
	}
	billingType = strings.ToUpper(strings.TrimSpace(billingType))
	method := payment.NormalizeMethod(billingType)

	customer, err := uc.charges.customer(input.UserID, method)
	if err != nil {
		return nil, err
	}

	exchangeRate := uc.getExchangeRate()

	amountUSD := int64(math.Round(input.AmountBRL / exchangeRate * 1_000_000))

	creditableUSD := amountUSD
	if purpose == invoice.PurposeMonthlyBilling {
		planUSD := int64(math.Round(input.CreditableBRL / exchangeRate * 1_000_000))
		creditableUSD = max(0, min(amountUSD, planUSD))
	}

	invoiceID := uuid.New().String()
	externalRef := invoice.ExternalReference(invoiceID)

	dueDate := time.Now().Add(invoice.DefaultChargeLifetime)
	if input.DueDate != nil {
		dueDate = *input.DueDate
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		switch purpose {
		case invoice.PurposeSubscription:
			description = "Assinatura de plano"
		case invoice.PurposeMonthlyBilling:
			description = "Cobrança mensal " + brand.Active().Name
		default:
			description = "Recarga de saldo"
		}
	}

	if method == payment.MethodBoleto && uc.gateway.Capabilities().BoletoRequiresAddress && !customer.Address.Complete() {
		log.Printf("[invoice] boleto rejected for user %s: address incomplete (missing %v)",
			input.UserID, payment.MissingAddressFields(customer.Address))
		return nil, invoice.ErrBillingAddressRequired
	}

	chargeReq := payment.ChargeRequest{
		Method:            method,
		Amount:            input.AmountBRL,
		DueDate:           dueDate,
		ExpiresAt:         chargeExpiry(purpose, input.DueDate),
		Description:       chargeDescription(purpose, description),
		ExternalReference: externalRef,
		Customer:          customer,
		IdempotencyKey:    externalRef,
	}

	var planDefinitionIDPtr *string
	if planDefinitionID != "" {
		planDefinitionIDPtr = &planDefinitionID
	}

	reserved, isNew, err := uc.invoiceRepo.Reserve(&invoice.Invoice{
		ID:               invoiceID,
		WorkspaceID:      input.WorkspaceID,
		UserID:           input.UserID,
		Purpose:          purpose,
		PlanDefinitionID: planDefinitionIDPtr,
		AmountBRL:        input.AmountBRL,
		AmountUSD:        amountUSD,
		CreditableUSD:    creditableUSD,
		ExchangeRate:     exchangeRate,
		Status:           invoice.StatusPending,
		BillingType:      billingType,
		BillingCycle:     input.BillingCycle,
		Provider:         uc.gateway.Provider(),
		IdempotencyKey:   strings.TrimSpace(input.IdempotencyKey),
		Description:      description,
		DueDate:          input.DueDate,
		LineItems:        input.LineItems,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save invoice: %w", err)
	}
	if !isNew {
		if reserved.WorkspaceID != input.WorkspaceID {
			return nil, invoice.ErrInvalidIdempotencyKey
		}
		return &invoice.CreateInvoiceOutput{Invoice: reserved}, nil
	}

	uc.attributeReferralIfNew(input.WorkspaceID, input.UserID, input.ReferralCode)

	chargeReq.Splits = uc.charges.splits(input.WorkspaceID)

	createdPayment, err := uc.gateway.CreateCharge(context.Background(), chargeReq)
	if err != nil {
		if cancelErr := uc.invoiceRepo.UpdateStatus(reserved.ID, invoice.StatusCancelled); cancelErr != nil {
			log.Printf("[invoice] could not cancel reservation %s after the gateway refused it; the stale sweep will: %v", reserved.ID, cancelErr)
		}
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	attachment := chargeAttachment(createdPayment, chargeReq.ExpiresAt)
	if err := uc.invoiceRepo.AttachCharge(reserved.ID, attachment); err != nil {
		return nil, fmt.Errorf("failed to attach charge %s to invoice %s: %w", createdPayment.ID, reserved.ID, err)
	}
	reserved.ExternalID = attachment.ExternalID
	reserved.PixQrCode = attachment.PixQrCode
	reserved.PixCopy = attachment.PixCopy
	reserved.BankSlipUrl = attachment.BankSlipUrl
	reserved.InvoiceUrl = attachment.InvoiceUrl
	reserved.ChargeExpiresAt = attachment.ExpiresAt

	return &invoice.CreateInvoiceOutput{Invoice: reserved}, nil
}

func (uc *createInvoiceUseCase) ensureRechargeAllowed(workspaceID string) error {
	if workspaceID == "" || uc.currentSubscription == nil {
		return invoice.ErrActiveSubscriptionRequired
	}
	_, err := uc.currentSubscription.Execute(workspaceID)
	if err == nil {
		return nil
	}
	if errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) || errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
		return invoice.ErrActiveSubscriptionRequired
	}
	return err
}

func (uc *createInvoiceUseCase) getExchangeRate() float64 {
	defaults, err := uc.exchangeRateRepo.ListDefaultPricingItems()
	if err != nil {
		log.Printf("[invoice] WARNING: failed to fetch exchange rate, using fallback %.1f: %v", workspace_pricing.DefaultUSDToBRL, err)
		return workspace_pricing.DefaultUSDToBRL
	}
	return workspace_pricing.USDToBRLRate(defaults)
}

func (uc *createInvoiceUseCase) attributeReferralIfNew(workspaceID, userID, rawCode string) {
	code := strings.TrimSpace(rawCode)
	if code == "" || uc.trackReferral == nil || uc.affiliateRepo == nil || strings.TrimSpace(workspaceID) == "" {
		return
	}
	ctx := context.Background()

	if existing, err := uc.affiliateRepo.GetReferralByWorkspaceID(ctx, workspaceID); err == nil && existing != nil {
		return
	}

	if _, err := uc.trackReferral.Execute(ctx, affiliate.TrackReferralInput{
		Code:                 code,
		WorkspaceID:          workspaceID,
		WorkspaceOwnerUserID: userID,
	}); err != nil {
		log.Printf("[invoice] referral attribution on conversion failed ws=%s code=%s: %v", workspaceID, code, err)
	}
}
