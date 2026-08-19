package balance_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/balance"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type consumeWhatsappTemplateUseCase struct {
	balanceRepo balance.Repository
	pricer      workspace_pricing.Pricer
	checker     workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
}

func NewConsumeWhatsappTemplateUseCase(balanceRepo balance.Repository, pricer workspace_pricing.Pricer, checker workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase) balance.ConsumeWhatsappTemplateUseCase {
	return &consumeWhatsappTemplateUseCase{balanceRepo: balanceRepo, pricer: pricer, checker: checker}
}

func (uc *consumeWhatsappTemplateUseCase) ensureCurrentSubscription(workspaceID string) error {
	if uc.checker == nil {
		return fmt.Errorf("current subscription checker is required")
	}
	_, err := uc.checker.Execute(workspaceID)
	return err
}

func (uc *consumeWhatsappTemplateUseCase) GetTemplateCostMicros(workspaceID string, templateCategory string) (int64, error) {
	if err := uc.ensureCurrentSubscription(workspaceID); err != nil {
		return 0, err
	}
	result, err := uc.pricer.PriceWhatsApp(workspaceID, templateCategory)
	if err != nil {
		return 0, fmt.Errorf("failed to get WhatsApp pricing: %w", err)
	}
	if result.PriceMicros <= 0 {
		// The pricer answers a missing item with a zero price and no error, so a
		// caller that only checked err would reserve nothing, debit nothing, and
		// send anyway.
		return 0, fmt.Errorf("%w: whatsapp template %s", balance.ErrPriceUnavailable, strings.ToLower(templateCategory))
	}

	return result.PriceMicros, nil
}

func (uc *consumeWhatsappTemplateUseCase) Refund(workspaceID string, referenceID string, templateCategory string) error {
	result, err := uc.pricer.PriceWhatsApp(workspaceID, templateCategory)
	if err != nil {
		return fmt.Errorf("failed to get WhatsApp pricing for refund: %w", err)
	}
	if result.PriceMicros <= 0 {
		// Returning nil here reported a refund that never happened, and the caller
		// then recorded the attempt as refunded — money kept, with a record saying
		// it was returned.
		return fmt.Errorf("%w: cannot refund whatsapp template %s", balance.ErrPriceUnavailable, strings.ToLower(templateCategory))
	}
	refRef := "refund:" + referenceID
	description := fmt.Sprintf("Reembolso: template WhatsApp %s não enviado (ref: %s)", strings.ToLower(templateCategory), referenceID)
	_, err = uc.balanceRepo.CreditBalance(balance.CreditBalanceInput{
		WorkspaceID:  workspaceID,
		Amount:       result.PriceMicros,
		ServiceType:  balance.ServiceWhatsAppCampaign,
		ReferenceID:  &refRef,
		Description:  description,
		CostMicros:   result.CostMicros,
		ProfitMicros: -result.ProfitMicros,
		IsRefund:     true,
	})
	return err
}

func (uc *consumeWhatsappTemplateUseCase) Execute(workspaceID string, referenceID string, templateCategory string) (*balance.Transaction, error) {
	if err := uc.ensureCurrentSubscription(workspaceID); err != nil {
		return nil, err
	}
	result, err := uc.pricer.PriceWhatsApp(workspaceID, templateCategory)
	if err != nil {
		return nil, fmt.Errorf("failed to get WhatsApp pricing: %w", err)
	}

	if result.PriceMicros <= 0 {
		// Fail CLOSED. This used to return (nil, nil): success-shaped, no charge —
		// so an unpriced workspace sent paid templates for free, at bulk volume,
		// with nothing logged. Every caller read the nil error as "billed".
		//
		// Guarding here rather than in each caller is deliberate: there are four
		// senders and only one of them had the check.
		return nil, fmt.Errorf("%w: whatsapp template %s", balance.ErrPriceUnavailable, strings.ToLower(templateCategory))
	}

	description := fmt.Sprintf("Template WhatsApp %s (ref: %s)", strings.ToLower(templateCategory), referenceID)
	return uc.balanceRepo.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:  workspaceID,
		Amount:       result.PriceMicros,
		ServiceType:  balance.ServiceWhatsAppCampaign,
		ReferenceID:  &referenceID,
		Description:  description,
		CostMicros:   result.CostMicros,
		ProfitMicros: result.ProfitMicros,
	})
}
