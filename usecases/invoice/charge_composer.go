package invoice_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/address"
	"vozko/domain/affiliate"
	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/domain/user"
)

type chargeComposer struct {
	userRepo      user.UserRepository
	addressRepo   address.AddressRepository
	gateway       payment.Gateway
	affiliateRepo affiliate.Repository
}

func (c *chargeComposer) customer(userID string, method payment.Method) (payment.GatewayCustomer, error) {
	u, err := c.userRepo.FindByID(userID)
	if err != nil {
		return payment.GatewayCustomer{}, fmt.Errorf("user not found: %w", err)
	}
	if u == nil {
		return payment.GatewayCustomer{}, fmt.Errorf("user not found: %s", userID)
	}

	document := strings.TrimSpace(u.CPF)
	if document == "" {
		document = strings.TrimSpace(u.CNPJ)
	}
	if document == "" {
		return payment.GatewayCustomer{}, invoice.ErrCustomerDocumentRequired
	}

	name := u.Username
	if name == "" {
		name = u.Email
	}
	return payment.GatewayCustomer{
		Name:     name,
		Email:    strings.TrimSpace(u.Email),
		Document: document,
		Address:  c.billingAddress(method, userID),
	}, nil
}

func chargeDescription(purpose invoice.Purpose, description string) string {
	switch purpose.Normalize() {
	case invoice.PurposeSubscription:
		return fmt.Sprintf("Assinatura de plano - %s", description)
	case invoice.PurposeMonthlyBilling:
		return fmt.Sprintf("Cobrança mensal %s - %s", brand.Active().Name, description)
	default:
		return fmt.Sprintf("Recarga de saldo - %s", description)
	}
}

func chargeExpiry(purpose invoice.Purpose, dueDate *time.Time) time.Time {
	until, bounded := invoice.PayableUntil(purpose, dueDate)
	if !bounded {
		return time.Time{}
	}
	return until
}

func chargeAttachment(charge *payment.Charge, requestedExpiry time.Time) invoice.ChargeAttachment {
	expiresAt := charge.ExpiresAt
	if expiresAt == nil && !requestedExpiry.IsZero() {
		requested := requestedExpiry
		expiresAt = &requested
	}
	return invoice.ChargeAttachment{
		ExternalID:  charge.ID,
		PixQrCode:   optionalText(charge.PixQRCodeBase64),
		PixCopy:     optionalText(charge.PixCopyPaste),
		BankSlipUrl: optionalText(charge.BoletoURL),
		InvoiceUrl:  optionalText(charge.InvoiceURL),
		ExpiresAt:   expiresAt,
	}
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (c *chargeComposer) splits(workspaceID string) []payment.SplitRecipient {
	split := c.affiliateSplit(workspaceID)
	if split == nil {
		return nil
	}
	if !c.gateway.Capabilities().Split {
		log.Printf("[invoice] WARNING: provider %s cannot split charges; affiliate commission for workspace %s must be paid out manually",
			c.gateway.Provider(), workspaceID)
		return nil
	}
	return []payment.SplitRecipient{*split}
}

func (c *chargeComposer) affiliateSplit(workspaceID string) *payment.SplitRecipient {
	if c.affiliateRepo == nil || strings.TrimSpace(workspaceID) == "" {
		return nil
	}
	ctx := context.Background()
	ref, err := c.affiliateRepo.GetReferralByWorkspaceID(ctx, workspaceID)
	if err != nil || ref == nil {
		return nil
	}
	aff, err := c.affiliateRepo.GetByID(ctx, ref.AffiliateID)
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

func (c *chargeComposer) billingAddress(method payment.Method, userID string) *payment.GatewayAddress {
	if method != payment.MethodBoleto || c.addressRepo == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	addresses, err := c.addressRepo.GetAllByUserID(userID)
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
