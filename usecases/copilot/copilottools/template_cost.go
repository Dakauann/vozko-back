package copilottools

import (
	"vozko/domain/balance"
	"vozko/domain/copilot"
	tmpl "vozko/domain/whatsapp/template"
	template_usecase "vozko/usecases/whatsapp/template"
)

const costUnavailable = "indisponível"

func templateCostFields(
	costs tmpl.TemplateCostReader,
	balances balance.BalanceReader,
	workspaceID string,
	template *tmpl.Template,
	eligible int64,
	costKey string,
	formatCost func(int64) string,
) []copilot.Field {
	quote, err := template_usecase.QuoteSend(costs, balances, workspaceID, template, eligible)
	if err != nil {
		return []copilot.Field{{Key: costKey, Value: costUnavailable}}
	}
	return []copilot.Field{
		{Key: costKey, Value: formatCost(quote.CostMicros)},
		{Key: "balance", Value: formatUSD(quote.BalanceMicros)},
	}
}
