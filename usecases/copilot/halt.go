package copilot_usecase

import (
	"errors"

	"vozko/usecases/agentloop"
)

const (
	AnswerTokenBudget = 200_000

	msgFundsExhausted  = "Saldo ou plano insuficiente: a resposta foi interrompida antes de gerar mais custo."
	msgBudgetExhausted = "A análise ficou longa demais e foi interrompida. Peça uma parte de cada vez."
)

func haltMessage(halt error) string {
	switch {
	case halt == nil:
		return ""
	case errors.Is(halt, ErrFundsExhausted):
		return msgFundsExhausted
	case errors.Is(halt, agentloop.ErrSessionBudget):
		return msgBudgetExhausted
	}
	return ""
}
