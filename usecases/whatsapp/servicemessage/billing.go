// Package servicemessage bills the free-form replies Meta starts charging for
// on 1 October 2026: the message an agent or the AI sends inside the 24 hour
// customer service window.
//
// It sits under usecases/whatsapp rather than under usecases/balance because
// what it knows is WhatsApp: Meta's pricing categories, what the status webhook
// stamps on a delivered message, which deliveries Meta exempts. The balance
// package is the ledger it writes to, not the subject.
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

// Meta's rule is that the charge lands on DELIVERY, so this books the money
// when Meta confirms it billed us, not when we hand the message over. That is
// why there is no refund path: we only ever record what Meta has already
// charged, so there is nothing to reverse. Charging at send would have meant a
// debit and a refund at each of the fifteen places that persist an outbound
// WhatsApp message, and none of them can see the two exemptions that matter,
// because only Meta knows about them:
//
//   - a delivery inside the 72 hour free entry point, which arrives with
//     billable false;
//   - a reply the owner typed in the WhatsApp Business app on a coexistence
//     number, which is not an API send and carries no billable pricing at all.
//
// The gate is the other half. Charging after delivery cannot refuse anything,
// so AllowSend runs before the send and asks the one question a plan can answer
// in advance: does this workspace pay for service messages, and can it.

// ErrBillingNotConfigured is returned by the constructor when a dependency is
// missing, rather than at send time.
//
// Every one of these dependencies decides money. A billing that quietly
// degrades to "allow and charge nothing" when one of them is absent is a
// revenue leak that no test fails on and no log mentions, so this refuses to
// exist instead. Mirrors template.ErrBillingNotConfigured.
var ErrBillingNotConfigured = errors.New("whatsapp service message billing is not configured")

// Pricer is the slice of the workspace pricer this needs. Narrow on purpose:
// the full Pricer has six methods and a fake for it would be five dead stubs.
type Pricer interface {
	PriceWhatsAppCategory(workspaceID string, metaCategory string) (workspace_pricing.PriceResult, error)
}

// Ledger is the slice of the balance repository this needs.
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

// NewBilling wires the billing, refusing to build when anything that decides
// money is missing.
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
		// Required, not optional: this constructor has already proven the
		// checker is there, so a nil one later is a bug and must refuse rather
		// than wave the spend through.
		guard: balance_usecase.NewRequiredSpendGuard(deps.BalanceChecker, "whatsapp service message"),
	}, nil
}

// price is what one service message costs this workspace right now, resolved
// through the ordinary catalog chain: platform default, then the plan, then any
// workspace override.
func (b Billing) price(workspaceID string) (workspace_pricing.PriceResult, error) {
	return b.pricer.PriceWhatsAppCategory(workspaceID, conversation.MetaPricingCategoryService)
}

// AllowSend reports whether this workspace may send one more free-form reply.
//
// A plan that prices service messages at zero is never refused, whatever the
// balance says, because there is nothing to pay for. That is the entire
// behaviour of the zero price and it needs no special case anywhere else: an
// unpriced workspace simply never reaches the balance read.
//
// A plan that does price them is held to it, against the cached balance.
func (b Billing) AllowSend(workspaceID string) error {
	result, err := b.price(workspaceID)
	if err != nil {
		// The price could not be resolved, which means the pricing chain is
		// down. Refusing matches the stance every other paid path takes: not
		// being able to read what something costs is not permission to give it
		// away.
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

// ShouldCharge reports whether this receipt is one we would charge for, using
// only what the receipt itself carries.
//
// Every clause is a way of charging for something Meta did not, so each is a
// test. The billability decision is NOT re-derived here: it reads the
// predicates on the receipt, which are the same ones the cost report and the
// partial index are built from.
//
// It is separate from ChargeDelivered so the status webhook can ask it before
// paying three database reads to resolve a workspace. ChargeDelivered asks it
// too, so a caller that skips the question cannot charge anything it should
// not.
func (b Billing) ShouldCharge(receipt conversation.DeliveryReceipt) bool {
	// Meta's own verdict, not our inference. A template category, a free
	// customer service message and a free entry point delivery all fail here.
	if !receipt.Pricing.IsBillableService() {
		return false
	}
	// Meta charges on delivery, so a failed send costs nothing however it was
	// categorised.
	if !receipt.Status.IsMetaBillable() {
		return false
	}
	// Belt and braces on the entry point. Meta already says billable false for
	// it, so reaching this line would mean Meta contradicted itself, and the
	// cheaper mistake is not to charge.
	return !receipt.IsFreeEntryPoint()
}

// ChargeDelivered books one service message Meta has confirmed it billed.
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
		// The plan does not charge for service messages. We still absorbed the
		// cost at Meta; that is the decision the zero price expresses.
		return nil
	}

	// Meta retries status webhooks, and a message passes through sent,
	// delivered and read. The provider message id is the one thing that is
	// stable across all of them.
	reference := Reference(providerMessageID)
	charged, err := b.ledger.ExistsTransactionByReferenceID(reference)
	if err != nil {
		return fmt.Errorf("whatsapp service message idempotency check: %w", err)
	}
	if charged {
		return nil
	}

	_, err = b.ledger.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:  workspaceID,
		Amount:       result.PriceMicros,
		ServiceType:  balance.ServiceWhatsAppConversation,
		ReferenceID:  &reference,
		Description:  fmt.Sprintf("Mensagem de serviço WhatsApp (ref: %s)", providerMessageID),
		CostMicros:   result.CostMicros,
		ProfitMicros: result.ProfitMicros,
		// The message is already delivered and Meta has already charged us.
		// Refusing the debit here would not un-send it, it would only lose the
		// record of money we owe.
		AllowNegative: true,
	})
	return err
}

// Reference is the ledger reference for one delivered service message.
// Prefixed so it cannot collide with a campaign id, a refund or a template send
// attempt, which share the reference column.
func Reference(providerMessageID string) string {
	return "wa-service:" + providerMessageID
}
