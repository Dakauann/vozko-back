package whatsapp_outreach

import (
	"context"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
)

// quoteUseCase answers what a send will cost, before the operator commits.
//
// It exists because this is the only dialog in the product that spends money on
// submit, and an operator who cannot see the price until the ledger moves has
// been asked to agree to something they were never shown. It reuses the SAME
// pricing call the charge itself uses — a second price source would eventually
// quote one number and charge another.
type quoteUseCase struct {
	templates template.Repository
	cost      template.TemplateCostReader
	balances  balance.CachedBalanceChecker
}

func NewQuoteUseCase(
	templates template.Repository,
	cost template.TemplateCostReader,
	balances balance.CachedBalanceChecker,
) wo.QuoteTemplateSendUseCase {
	return &quoteUseCase{templates: templates, cost: cost, balances: balances}
}

func (uc *quoteUseCase) Execute(ctx context.Context, workspaceID, templateID, businessPhoneID string) (*wo.SendQuote, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, template.ErrWorkspaceRequired
	}
	tmpl, err := uc.templates.FindByID(templateID)
	if err != nil || tmpl == nil {
		return nil, wo.ErrTemplateNotFound
	}
	category, err := tmpl.BillingCategory()
	if err != nil {
		return nil, err
	}

	// Deliberately the same call the debit makes, including its subscription
	// check — so a lapsed plan surfaces here, in the dialog, rather than as a
	// failure after the operator has filled in every field.
	priceMicros, err := uc.cost.GetTemplateCostMicros(workspaceID, category)
	if err != nil {
		return nil, err
	}
	if priceMicros <= 0 {
		return nil, template.ErrPricingUnavailable
	}

	quote := &wo.SendQuote{Category: category, PriceMicros: priceMicros}
	if uc.balances != nil {
		if current, balErr := uc.balances.GetBalance(workspaceID); balErr == nil {
			quote.BalanceMicros = current
			quote.Affordable = current >= priceMicros
		}
	}
	return quote, nil
}
