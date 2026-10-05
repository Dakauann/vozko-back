package servicemessage

import (
	"errors"
	"fmt"
	"strings"
	"time"

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
	Unattributed   conversation.UnattributedServiceMessageRepository
}

type Billing struct {
	pricer       Pricer
	ledger       Ledger
	guard        balance_usecase.SpendGuard
	unattributed conversation.UnattributedServiceMessageRepository
	now          func() time.Time
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
	if deps.Unattributed == nil {
		missing = append(missing, "unattributed service message log")
	}
	if len(missing) > 0 {
		return Billing{}, fmt.Errorf("%w: %s", ErrBillingNotConfigured, strings.Join(missing, ", "))
	}
	return Billing{
		pricer:       deps.Pricer,
		ledger:       deps.Ledger,
		guard:        balance_usecase.NewRequiredSpendGuard(deps.BalanceChecker, "whatsapp service message"),
		unattributed: deps.Unattributed,
		now:          time.Now,
	}, nil
}

func (b Billing) RecordUnattributed(providerMessageID, phoneNumberID string, receipt conversation.DeliveryReceipt) error {
	return b.unattributed.Record(conversation.NewUnattributedServiceMessage(providerMessageID, phoneNumberID, receipt, b.now().UTC()))
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

	_, err = balance_usecase.DebitOnce(b.ledger, balance_usecase.ReferenceCharge{
		WorkspaceID:   workspaceID,
		ReferenceID:   Reference(providerMessageID),
		ServiceType:   balance.ServiceWhatsAppConversation,
		Price:         result,
		Description:   fmt.Sprintf("Mensagem de serviço WhatsApp (ref: %s)", providerMessageID),
		AllowNegative: true,
	})
	return err
}

func Reference(providerMessageID string) string {
	return "wa-service:" + providerMessageID
}
