package template_usecase

import (
	"fmt"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
)

func PriceOf(costs template.TemplateCostReader, workspaceID string, t *template.Template) (string, int64, error) {
	category, err := t.BillingCategory()
	if err != nil {
		return "", 0, err
	}
	if costs == nil {
		return "", 0, fmt.Errorf("%w: template price reader is missing", template.ErrBillingNotConfigured)
	}
	unit, err := costs.GetTemplateCostMicros(workspaceID, category)
	if err != nil {
		return "", 0, fmt.Errorf("template price: %w", err)
	}
	if unit <= 0 {
		return category, 0, template.ErrPricingUnavailable
	}
	return category, unit, nil
}

func QuoteSend(costs template.TemplateCostReader, balances balance.BalanceReader, workspaceID string, t *template.Template, eligible int64) (template.SendCost, error) {
	category, unit, err := PriceOf(costs, workspaceID, t)
	if err != nil {
		return template.SendCost{}, err
	}
	if balances == nil {
		return template.SendCost{}, fmt.Errorf("%w: balance reader is missing", template.ErrBillingNotConfigured)
	}
	current, err := balances.GetBalance(workspaceID)
	if err != nil {
		return template.SendCost{}, fmt.Errorf("balance: %w", err)
	}
	quote, err := template.Quote(unit, eligible, current, balance.LedgerCurrency)
	if err != nil {
		return template.SendCost{}, err
	}
	quote.Category = category
	return quote, nil
}
