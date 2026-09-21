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
	userRepo            user.UserRepository
	addressRepo         address.AddressRepository
	gateway             payment.Gateway
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
		invoiceRepo:         invoiceRepo,
		userRepo:            userRepo,
		addressRepo:         addressRepo,
		gateway:             gateway,
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
	if cpf == "" {
		return nil, invoice.ErrCustomerDocumentRequired
	}

	customerName := u.Username
	if customerName == "" {
		customerName = u.Email
	}

	method := payment.NormalizeMethod(billingType)

	billingAddress := uc.billingAddress(method, input.UserID)
	if method == payment.MethodBoleto && uc.gateway.Capabilities().BoletoRequiresAddress && !billingAddress.Complete() {
		log.Printf("[invoice] boleto rejected for user %s: address incomplete (missing %v)",
			input.UserID, payment.MissingAddressFields(billingAddress))
		return nil, invoice.ErrBillingAddressRequired
	}

	chargeReq := payment.ChargeRequest{
		Method:            method,
		Amount:            input.AmountBRL,
		DueDate:           dueDate,
		Description:       paymentDescription,
		ExternalReference: externalRef,
		Customer: payment.GatewayCustomer{
			Name:     customerName,
			Email:    strings.TrimSpace(u.Email),
			Document: cpf,
			Address:  billingAddress,
		},
		IdempotencyKey: externalRef,
	}

	uc.attributeReferralIfNew(input.WorkspaceID, input.UserID, input.ReferralCode)

	if split := uc.buildAffiliateSplit(input.WorkspaceID); split != nil {
		if uc.gateway.Capabilities().Split {
			chargeReq.Splits = []payment.SplitRecipient{*split}
		} else {
			log.Printf("[invoice] WARNING: provider %s cannot split charges; affiliate commission for workspace %s must be paid out manually",
				uc.gateway.Provider(), input.WorkspaceID)
		}
	}

	createdPayment, err := uc.gateway.CreateCharge(context.Background(), chargeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	var pixQrCode, pixCopy *string
	if createdPayment.PixQRCodeBase64 != "" {
		qr := createdPayment.PixQRCodeBase64
		pixQrCode = &qr
	}
	if createdPayment.PixCopyPaste != "" {
		copyPaste := createdPayment.PixCopyPaste
		pixCopy = &copyPaste
	}

	var bankSlipUrl, invoiceUrl *string
	if createdPayment.BoletoURL != "" {
		slip := createdPayment.BoletoURL
		bankSlipUrl = &slip
	}
	if createdPayment.InvoiceURL != "" {
		hosted := createdPayment.InvoiceURL
		invoiceUrl = &hosted
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

func (uc *createInvoiceUseCase) buildAffiliateSplit(workspaceID string) *payment.SplitRecipient {
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

	return &payment.SplitRecipient{
		RecipientID: aff.AsaasWalletID,
		Percentage:  aff.CommissionPct * 100,
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

func (uc *createInvoiceUseCase) billingAddress(method payment.Method, userID string) *payment.GatewayAddress {
	if method != payment.MethodBoleto || uc.addressRepo == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	addresses, err := uc.addressRepo.GetAllByUserID(userID)
	if err != nil || len(addresses) == 0 {
		if err != nil {
			log.Printf("[invoice] WARNING: could not load billing address for user %s: %v", userID, err)
		}
		return nil
	}

	chosen := addresses[0]
	for _, a := range addresses {
		if a != nil && a.IsDefault {
			chosen = a
			break
		}
	}
	if chosen == nil {
		return nil
	}
	return &payment.GatewayAddress{
		ZipCode:      chosen.ZipCode,
		StreetName:   chosen.Street,
		StreetNumber: chosen.Number,
		Neighborhood: chosen.District,
		City:         chosen.City,
		FederalUnit:  chosen.State,
	}
}
