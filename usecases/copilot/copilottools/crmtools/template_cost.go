package crmtools

import (
	"vozko/domain/balance"
	"vozko/domain/copilot"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/usecases/copilot/copilottools"
	template_usecase "vozko/usecases/whatsapp/template"
)

const costUnavailable = "indisponível"

func templateCostFields(
	costs tmpl.TemplateCostReader,
	balances balance.BalanceReader,
	rates balance.ExchangeRate,
	workspaceID string,
	template *tmpl.Template,
	eligible int64,
	costKey string,
) []copilot.Field {
	quote, err := template_usecase.QuoteSend(costs, balances, workspaceID, template, eligible)
	reais, priced := copilottools.ReaisOf(rates)
	if err != nil || !priced {
		return []copilot.Field{{Key: costKey, Value: costUnavailable}}
	}
	return []copilot.Field{
		{Key: costKey, Value: reais(quote.CostMicros)},
		{Key: "balance", Value: reais(quote.BalanceMicros)},
	}
}
