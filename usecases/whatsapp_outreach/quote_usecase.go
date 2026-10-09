package whatsapp_outreach

import (
	"context"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace_template_access"
	template_usecase "vozko/usecases/whatsapp/template"
)

type TemplateFinder interface {
	FindByID(templateID string) (*template.Template, error)
}

type quoteUseCase struct {
	templates TemplateFinder
	grant     workspace_template_access.CheckAccessUseCase
	cost      template.TemplateCostReader
	balances  balance.BalanceReader
}

func NewQuoteUseCase(
	templates TemplateFinder,
	grant workspace_template_access.CheckAccessUseCase,
	cost template.TemplateCostReader,
	balances balance.BalanceReader,
) wo.QuoteTemplateSendUseCase {
	return &quoteUseCase{templates: templates, grant: grant, cost: cost, balances: balances}
}

func (uc *quoteUseCase) Execute(ctx context.Context, workspaceID, templateID, businessPhoneID string) (*wo.SendQuote, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, template.ErrWorkspaceRequired
	}
	tmpl, err := grantedTemplate(uc.templates, uc.grant, workspaceID, templateID)
	if err != nil {
		return nil, err
	}
	cost, err := template_usecase.QuoteSend(uc.cost, uc.balances, workspaceID, tmpl, 1)
	if err != nil {
		return nil, err
	}
	return &wo.SendQuote{
		Category:      cost.Category,
		PriceMicros:   cost.UnitPriceMicros,
		BalanceMicros: cost.BalanceMicros,
		Affordable:    cost.Affordable,
	}, nil
}
