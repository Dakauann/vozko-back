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

	return result.PriceMicros, nil
}

func (uc *consumeWhatsappTemplateUseCase) Refund(workspaceID string, referenceID string, templateCategory string) error {
	result, err := uc.pricer.PriceWhatsApp(workspaceID, templateCategory)
	if err != nil {
		return fmt.Errorf("failed to get WhatsApp pricing for refund: %w", err)
	}
	if result.PriceMicros <= 0 {
		return nil
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
		return nil, nil
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
