package platformtools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

type askUserTool struct{}

func NewAskUserTool() copilot.Tool { return askUserTool{} }

func (askUserTool) OfferedOn(view copilot.View) bool { return !view.OnStudio() }

func (askUserTool) Meta() copilot.Meta { return copilottools.ChatReadMeta() }

func (askUserTool) Definition() tools.Definition {
	return copilottools.Definition("ask_user",
		"Mostra ao usuário um cartão com 1 a 4 perguntas, cada uma com 2 a 4 opções que ele toca (com detalhe curto opcional) ou uma resposta escrita, "+
			"e encerra a sua resposta para esperar. Use quando a escolha dele muda o caminho, por exemplo como criar ou editar um criativo. "+
			"Junte numa só chamada o que precisa saber e não pergunte o que ele já disse.",
		copilottools.AskArgs{})
}

func (askUserTool) Execute(_ context.Context, _ copilot.Context, args map[string]interface{}) copilot.Result {
	var a copilottools.AskArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return copilottools.AskResult(a)
}
