package balance_usecase

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/balance"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

var errMonthlySendCapReaderMissing = errors.New("monthly send cap reader is required")

type consumeWhatsappTemplateUseCase struct {
	balanceRepo balance.Repository
	pricer      workspace_pricing.Pricer
	checker     workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	caps        balance.MonthlySendCapReader
	now         func() time.Time
}

func NewConsumeWhatsappTemplateUseCase(balanceRepo balance.Repository, pricer workspace_pricing.Pricer, checker workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase, caps balance.MonthlySendCapReader) balance.ConsumeWhatsappTemplateUseCase {
	return &consumeWhatsappTemplateUseCase{balanceRepo: balanceRepo, pricer: pricer, checker: checker, caps: caps, now: time.Now}
}

func (uc *consumeWhatsappTemplateUseCase) monthlyCapGuard(workspaceID string) (*balance.MonthlySendCapGuard, error) {
	if uc.caps == nil {
		return nil, errMonthlySendCapReaderMissing
	}
	cap, err := uc.caps.GetMonthlySendCap(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to read monthly send cap: %w", err)
	}
	if cap == nil {
		return nil, nil
	}
	guard := cap.Guard(uc.now())
	return &guard, nil
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
		return nil, fmt.Errorf("%w: whatsapp template %s", balance.ErrPriceUnavailable, strings.ToLower(templateCategory))
	}

	monthlyCap, err := uc.monthlyCapGuard(workspaceID)
	if err != nil {
		return nil, err
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
		MonthlyCap:   monthlyCap,
	})
}
