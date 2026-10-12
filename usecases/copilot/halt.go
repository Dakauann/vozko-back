package copilot_usecase

import (
	"errors"
	"fmt"
	"math"

	"vozko/domain/ai"
	"vozko/usecases/agentloop"
)

const (
	AnswerTokenBudget        = 200_000
	answerMaxTokensPerGen    = 4000
	studioMaxTokensPerGen    = 24000
	studioReasoningMaxTokens = 5000
	answerGraceInstruction   = "Você chegou ao limite desta resposta. Sem chamar ferramentas, responda agora ao pedido com o que já descobriu e diga em uma frase o que ficou faltando verificar."

	msgFundsExhausted      = "Saldo ou plano insuficiente: a resposta foi interrompida antes de gerar mais custo."
	msgBudgetExhausted     = "A análise ficou longa demais e foi interrompida. Peça uma parte de cada vez."
	msgOutputTruncated     = "A resposta foi cortada pelo limite de saída do modelo antes de terminar. Peça para continuar que eu sigo em partes menores."
	msgRepeatedTurn        = "A Elo repetiu a mesma ação várias vezes seguidas sem avançar e parou para não gastar à toa. O que ela já fez ficou salvo; diga como seguir ou peça de outro jeito."
	msgProviderFailed      = "A Elo não conseguiu terminar esta resposta por uma falha interna. Tente de novo em alguns minutos."
	msgProviderUnreachable = "A conexão com o provedor de IA caiu e não voltou depois de algumas tentativas. O que a Elo já fez ficou salvo; tente de novo em instantes."
	msgProviderBusy        = "O provedor de IA está no limite de uso neste momento e recusou a resposta. O que a Elo já fez ficou salvo; tente de novo em %s."
	busyWaitUnknown        = "alguns minutos"
	busyWaitOneMinute      = "cerca de 1 minuto"
	busyWaitMinutes        = "cerca de %d minutos"

	ErrorCodeProviderUnreachable = "provider_unreachable"
	ErrorCodeProviderBusy        = "provider_busy"
)

func haltFailure(halt error) map[string]interface{} {
	msg := haltMessage(halt)
	if msg == "" {
		return nil
	}
	failure := map[string]interface{}{"error": msg}
	if code := haltCode(halt); code != "" {
		failure["code"] = code
	}
	if seconds := retryAfterSeconds(halt); seconds > 0 {
		failure["retryAfterSeconds"] = seconds
	}
	return failure
}

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
	case errors.Is(halt, agentloop.ErrRepeatedTurn):
		return msgRepeatedTurn
	case errors.Is(halt, ai.ErrProviderBusy):
		return fmt.Sprintf(msgProviderBusy, busyWait(retryAfterSeconds(halt)))
	case errors.Is(halt, agentloop.ErrProviderUnreachable):
		return msgProviderUnreachable
	case errors.Is(halt, agentloop.ErrProviderFailed):
		return msgProviderFailed
	}
	return ""
}

func haltCode(halt error) string {
	switch {
	case errors.Is(halt, ai.ErrProviderBusy):
		return ErrorCodeProviderBusy
	case errors.Is(halt, agentloop.ErrProviderUnreachable):
		return ErrorCodeProviderUnreachable
	}
	return ""
}

func retryAfterSeconds(halt error) int {
	wait, _ := ai.BusyRetryAfter(halt)
	return int(math.Ceil(wait.Seconds()))
}

func busyWait(seconds int) string {
	minutes := (seconds + 59) / 60
	switch {
	case minutes <= 0:
		return busyWaitUnknown
	case minutes == 1:
		return busyWaitOneMinute
	}
	return fmt.Sprintf(busyWaitMinutes, minutes)
}
