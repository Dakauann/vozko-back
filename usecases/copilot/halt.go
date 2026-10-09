package copilot_usecase

import (
	"errors"

	"vozko/usecases/agentloop"
)

const (
	AnswerTokenBudget              = 200_000
	DefaultAnswerCostCeilingMicros = int64(1_000_000)
	answerMaxIterations            = 60
	answerMaxTokensPerGen          = 4000
	studioMaxTokensPerGen          = 24000
	studioReasoningMaxTokens       = 5000
	answerGraceInstruction         = "Você chegou ao limite desta resposta. Sem chamar ferramentas, responda agora ao pedido com o que já descobriu e diga em uma frase o que ficou faltando verificar."

	msgFundsExhausted  = "Saldo ou plano insuficiente: a resposta foi interrompida antes de gerar mais custo."
	msgBudgetExhausted = "A análise ficou longa demais e foi interrompida. Peça uma parte de cada vez."
	msgOutputTruncated = "A resposta foi cortada pelo limite de saída do modelo antes de terminar. Peça para continuar que eu sigo em partes menores."
	msgProviderFailed  = "A Elo não conseguiu terminar esta resposta por uma falha interna. Tente de novo em alguns minutos."
)

func haltMessage(halt error) string {
	switch {
	case halt == nil:
		return ""
	case errors.Is(halt, ErrFundsExhausted):
		return msgFundsExhausted
	case errors.Is(halt, agentloop.ErrSessionBudget):
		return msgBudgetExhausted
	case errors.Is(halt, agentloop.ErrOutputTruncated):
		return msgOutputTruncated
	case errors.Is(halt, agentloop.ErrProviderFailed):
		return msgProviderFailed
	}
	return ""
}
