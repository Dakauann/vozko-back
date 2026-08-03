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
	"vozko/domain/affiliate"
	"vozko/domain/invoice"
	"vozko/domain/user"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/asaas"

	"github.com/google/uuid"
)

type createInvoiceUseCase struct {
	invoiceRepo         invoice.Repository
	userRepo            user.UserRepository
	asaasService        asaas.AsaasServiceUseCases
	exchangeRateRepo    workspace_pricing.Repository
	currentSubscription workspace_plan.EnsureCurrentWorkspaceSubscriptionUseCase
	affiliateRepo       affiliate.Repository
	trackReferral       affiliate.TrackReferralUseCase
}

func NewCreateInvoiceUseCase(
	invoiceRepo invoice.Repository,
	userRepo user.UserRepository,
	asaasService asaas.AsaasServiceUseCases,
	exchangeRateRepo workspace_pricing.Repository,
	currentSubscription workspace_plan.EnsureCurrentWorkspaceSubscriptionUseCase,
	affiliateRepo affiliate.Repository,
	trackReferral affiliate.TrackReferralUseCase,
) invoice.CreateInvoiceUseCase {
	return &createInvoiceUseCase{
		invoiceRepo:         invoiceRepo,
		userRepo:            userRepo,
		asaasService:        asaasService,
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

	// Idempotency: if an invoice already exists for this key, return it without charging Asaas again.
	// This makes a monthly-emit re-run safe (no double charge).
	if key := strings.TrimSpace(input.IdempotencyKey); key != "" {
		existing, err := uc.invoiceRepo.GetByIdempotencyKey(key)
		if err != nil {
			return nil, fmt.Errorf("idempotency lookup failed: %w", err)
		}
		if existing != nil {
			return &invoice.CreateInvoiceOutput{Invoice: existing}, nil
		}
	}

	u, err := uc.userRepo.FindByID(input.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	exchangeRate := uc.getExchangeRate()

	amountUSD := int64(math.Round(input.AmountBRL / exchangeRate * 1_000_000))

	// CreditableUSD is the saldo to credit on payment. TOP_UP and SUBSCRIPTION credit the full
	// amount. MONTHLY_BILLING credits only the plan portion (CreditableBRL), so the channel-license
	// repasse never becomes saldo. It is converted at the SAME exchange rate as the total and is
	// clamped to [0, amountUSD] so a caller error can never credit more than was charged.
	creditableUSD := amountUSD
	if purpose == invoice.PurposeMonthlyBilling {
		planUSD := int64(math.Round(input.CreditableBRL / exchangeRate * 1_000_000))
		creditableUSD = max(0, min(amountUSD, planUSD))
	}

	invoiceID := uuid.New().String()
	externalRef := "inv:" + invoiceID

	billingType := input.BillingType
	if billingType == "" {
		billingType = "PIX"
	}
	billingType = strings.ToUpper(strings.TrimSpace(billingType))

	dueDate := time.Now().AddDate(0, 0, 3)
	if input.DueDate != nil {
		dueDate = *input.DueDate
	}
	dueDateStr := dueDate.Format("2006-01-02")
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

	var paymentDescription string
	switch purpose {
	case invoice.PurposeSubscription:
		paymentDescription = fmt.Sprintf("Assinatura de plano - %s", description)
	case invoice.PurposeMonthlyBilling:
		paymentDescription = fmt.Sprintf("Cobrança mensal %s - %s", brand.Active().Name, description)
	default:
		paymentDescription = fmt.Sprintf("Recarga de saldo - %s", description)
	}

	cpf := strings.TrimSpace(u.CPF)
	if cpf == "" {
		cpf = strings.TrimSpace(u.CNPJ)
	}
	// Asaas requires a document to create any charge. Without one, GetOrCreateCustomer would issue a
	// wildcard search (cpfCnpj=<empty>) that resolves to an unrelated document-less customer and then
	// fail, or worse, bill the wrong customer. Reject here so the caller gets a clear, actionable error.
	if cpf == "" {
		return nil, invoice.ErrCustomerDocumentRequired
	}

	customerName := u.Username
	if customerName == "" {
		customerName = u.Email
	}

	asaasPayment := &asaas.AsaasPayment{
		BillingType:       billingType,
		Value:             input.AmountBRL,
		DueDate:           dueDateStr,
		Description:       paymentDescription,
		ExternalReference: externalRef,
	}

	uc.attributeReferralIfNew(input.WorkspaceID, input.UserID, input.ReferralCode)

	if split := uc.buildAffiliateSplit(input.WorkspaceID); split != nil {
		asaasPayment.Split = []asaas.AsaasSplit{*split}
	}

	createdPayment, err := uc.asaasService.CreatePayment(customerName, cpf, asaasPayment)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	var pixQrCode, pixCopy *string
	if billingType == "PIX" {
		qr, copy, err := uc.asaasService.GetPaymentQrCode(createdPayment.ID)
		if err == nil {
			pixQrCode = &qr
			pixCopy = &copy
		}
	}

	var bankSlipUrl, invoiceUrl *string
	if createdPayment.BankSlipUrl != "" {
		bankSlipUrl = &createdPayment.BankSlipUrl
	}
	if createdPayment.InvoiceUrl != "" {
		invoiceUrl = &createdPayment.InvoiceUrl
	}

	var planDefinitionIDPtr *string
	if planDefinitionID != "" {
		planDefinitionIDPtr = &planDefinitionID
	}

	inv := &invoice.Invoice{
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
		ExternalID:       createdPayment.ID,
		IdempotencyKey:   strings.TrimSpace(input.IdempotencyKey),
		PixQrCode:        pixQrCode,
		PixCopy:          pixCopy,
		BankSlipUrl:      bankSlipUrl,
		InvoiceUrl:       invoiceUrl,
		Description:      description,
		DueDate:          input.DueDate,
		LineItems:        input.LineItems,
	}

	if err := uc.invoiceRepo.Create(inv); err != nil {
		return nil, fmt.Errorf("failed to save invoice: %w", err)
	}

	return &invoice.CreateInvoiceOutput{Invoice: inv}, nil
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

func (uc *createInvoiceUseCase) buildAffiliateSplit(workspaceID string) *asaas.AsaasSplit {
	if uc.affiliateRepo == nil || strings.TrimSpace(workspaceID) == "" {
		return nil
	}
	ctx := context.Background()
	ref, err := uc.affiliateRepo.GetReferralByWorkspaceID(ctx, workspaceID)
	if err != nil || ref == nil {
		return nil
	}
	aff, err := uc.affiliateRepo.GetByID(ctx, ref.AffiliateID)
	if err != nil || aff == nil || !aff.Active {
		return nil
	}
	if aff.CommissionPct <= 0 || aff.AsaasWalletID == "" {
		return nil
	}

	return &asaas.AsaasSplit{
		WalletID:        aff.AsaasWalletID,
		PercentualValue: aff.CommissionPct * 100,
	}
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
