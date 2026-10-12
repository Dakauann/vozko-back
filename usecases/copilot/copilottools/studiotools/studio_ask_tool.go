package studiotools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

type studioAskTool struct{ studioScope }

func NewStudioAskTool() copilot.Tool { return &studioAskTool{} }

func (t *studioAskTool) Meta() copilot.Meta { return copilottools.ChatReadMeta() }

func (t *studioAskTool) Definition() tools.Definition {
	return copilottools.Definition("studio_ask",
		"Mostra ao usuário um cartão com 1 a 4 perguntas, cada uma com 2 a 4 opções que ele toca (com detalhe curto opcional) ou uma resposta escrita, "+
			"e encerra a sua resposta para esperar. Com várias perguntas, as respostas chegam juntas na próxima mensagem. "+
			"Use antes de começar uma peça nova, antes de uma edição grande quando o objetivo, o formato, a duração, o tom ou o estilo não estiverem claros, "+
			"ou no fim para oferecer o próximo passo. Junte numa só chamada tudo o que precisa saber e não pergunte o que o usuário já disse.",
		copilottools.AskArgs{})
}

func (t *studioAskTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a copilottools.AskArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if !cc.View.OnStudio() {
		return studioFailure(studioNotHere)
	}
	return copilottools.AskResult(a)
}
