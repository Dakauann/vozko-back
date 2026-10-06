package balance_usecase

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/balance"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

var errMonthlySendSlotsMissing = errors.New("monthly send slots are required")

type consumeWhatsappTemplateUseCase struct {
	balanceRepo balance.Repository
	pricer      workspace_pricing.Pricer
	checker     workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	slots       balance.MonthlySendSlots
	now         func() time.Time
}

func NewConsumeWhatsappTemplateUseCase(balanceRepo balance.Repository, pricer workspace_pricing.Pricer, checker workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase, slots balance.MonthlySendSlots) balance.ConsumeWhatsappTemplateUseCase {
	return &consumeWhatsappTemplateUseCase{balanceRepo: balanceRepo, pricer: pricer, checker: checker, slots: slots, now: time.Now}
}

func (uc *consumeWhatsappTemplateUseCase) takeMonthlySendSlot(workspaceID, referenceID string) (bool, error) {
	if uc.slots == nil {
		return false, errMonthlySendSlotsMissing
	}
	took, err := uc.slots.TakeMonthlySendSlot(workspaceID, referenceID, uc.now())
	if err != nil {
		if errors.Is(err, balance.ErrMonthlySendCapReached) {
			return false, err
		}
		return false, fmt.Errorf("failed to take a monthly send slot: %w", err)
	}
	return took, nil
}

func (uc *consumeWhatsappTemplateUseCase) giveBackMonthlySendSlot(workspaceID, referenceID string) {
	if uc.slots == nil {
		return
	}
	if err := uc.slots.GiveBackMonthlySendSlot(workspaceID, referenceID); err != nil {
		log.Printf("[monthly-send-cap] slot %s of workspace %s stays taken: %v", referenceID, workspaceID, err)
	}
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
	if err := RefundOnce(uc.balanceRepo, templateCharge(workspaceID, referenceID, templateCategory, result)); err != nil {
		return err
	}
	uc.giveBackMonthlySendSlot(workspaceID, referenceID)
	return nil
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

	tookSlot, err := uc.takeMonthlySendSlot(workspaceID, referenceID)
	if err != nil {
		return nil, err
	}

	transaction, err := DebitOnce(uc.balanceRepo, templateCharge(workspaceID, referenceID, templateCategory, result))
	if err != nil {
		if tookSlot {
			uc.giveBackMonthlySendSlot(workspaceID, referenceID)
		}
		return nil, err
	}
	return transaction, nil
}

func templateCharge(workspaceID, referenceID, templateCategory string, price workspace_pricing.PriceResult) ReferenceCharge {
	category := strings.ToLower(templateCategory)
	return ReferenceCharge{
		WorkspaceID:       workspaceID,
		ReferenceID:       referenceID,
		ServiceType:       balance.ServiceWhatsAppCampaign,
		Price:             price,
		Description:       fmt.Sprintf("Template WhatsApp %s (ref: %s)", category, referenceID),
		RefundDescription: fmt.Sprintf("Reembolso: template WhatsApp %s não enviado (ref: %s)", category, referenceID),
	}
}
