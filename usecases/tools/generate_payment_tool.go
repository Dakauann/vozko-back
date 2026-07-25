package tools_usecase

import (
	"context"

	"vozko/domain/tools"
)

type generatePaymentTool struct{}

func NewGeneratePaymentToolUseCase() tools.Handler {
	return &generatePaymentTool{}
}

func (t *generatePaymentTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               "generate_payment",
		DisplayName:        "Gerar Pagamento",
		Description:        "Gera um link de pagamento para o cliente.",
		DisplayDescription: "Gera um link de pagamento para envio ao cliente.",
		Parameters:         map[string]tools.Parameter{},
		Visibility:         []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:           tools.CategoryPayment,
	}
}

func (t *generatePaymentTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{Result: "not implemented", IsError: true}, nil
}

func (t *generatePaymentTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.Execute(ctx, params)
}

var _ tools.Handler = (*generatePaymentTool)(nil)
