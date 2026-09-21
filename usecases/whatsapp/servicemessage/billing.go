package servicemessage

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	balance_usecase "vozko/usecases/balance"
)

var ErrBillingNotConfigured = errors.New("whatsapp service message billing is not configured")

type Pricer interface {
	PriceWhatsAppCategory(workspaceID string, metaCategory string) (workspace_pricing.PriceResult, error)
}

type Ledger interface {
	ExistsTransactionByReferenceID(referenceID string) (bool, error)
	DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error)
}

type Deps struct {
	Pricer         Pricer
	Ledger         Ledger
	BalanceChecker balance.CachedBalanceChecker
}

type Billing struct {
	pricer Pricer
	ledger Ledger
	guard  balance_usecase.SpendGuard
}

func NewBilling(deps Deps) (Billing, error) {
	var missing []string
	if deps.Pricer == nil {
		missing = append(missing, "whatsapp pricer")
	}
	if deps.Ledger == nil {
		missing = append(missing, "balance ledger")
	}
	if deps.BalanceChecker == nil {
		missing = append(missing, "cached balance checker")
	}
	if len(missing) > 0 {
		return Billing{}, fmt.Errorf("%w: %s", ErrBillingNotConfigured, strings.Join(missing, ", "))
	}
	return Billing{
		pricer: deps.Pricer,
		ledger: deps.Ledger,
		guard:  balance_usecase.NewRequiredSpendGuard(deps.BalanceChecker, "whatsapp service message"),
	}, nil
}

func (b Billing) price(workspaceID string) (workspace_pricing.PriceResult, error) {
	return b.pricer.PriceWhatsAppCategory(workspaceID, conversation.MetaPricingCategoryService)
}

func (b Billing) AllowSend(workspaceID string) error {
	result, err := b.price(workspaceID)
	if err != nil {
		return fmt.Errorf("whatsapp service message pricing: %w", err)
	}
	if result.PriceMicros <= 0 {
		return nil
	}
	if b.guard.CanAfford(workspaceID, result.PriceMicros) {
		return nil
	}
	return balance.ErrInsufficientBalance
}

func (b Billing) ShouldCharge(receipt conversation.DeliveryReceipt) bool {
	if !receipt.Pricing.IsBillableService() {
		return false
	}
	if !receipt.Status.IsMetaBillable() {
		return false
	}
	return !receipt.IsFreeEntryPoint()
}

func (b Billing) ChargeDelivered(
	workspaceID string,
	receipt conversation.DeliveryReceipt,
	providerMessageID string,
) error {
	workspaceID = strings.TrimSpace(workspaceID)
	providerMessageID = strings.TrimSpace(providerMessageID)
	if workspaceID == "" || providerMessageID == "" {
		return nil
	}
	if !b.ShouldCharge(receipt) {
		return nil
	}

	result, err := b.price(workspaceID)
	if err != nil {
		return fmt.Errorf("whatsapp service message pricing: %w", err)
	}
	if result.PriceMicros <= 0 {
		return nil
	}

	reference := Reference(providerMessageID)
	charged, err := b.ledger.ExistsTransactionByReferenceID(reference)
	if err != nil {
		return fmt.Errorf("whatsapp service message idempotency check: %w", err)
	}
	if charged {
		return nil
	}

	_, err = b.ledger.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:   workspaceID,
		Amount:        result.PriceMicros,
		ServiceType:   balance.ServiceWhatsAppConversation,
		ReferenceID:   &reference,
		Description:   fmt.Sprintf("Mensagem de serviço WhatsApp (ref: %s)", providerMessageID),
		CostMicros:    result.CostMicros,
		ProfitMicros:  result.ProfitMicros,
		AllowNegative: true,
	})
	return err
}

func Reference(providerMessageID string) string {
	return "wa-service:" + providerMessageID
}
