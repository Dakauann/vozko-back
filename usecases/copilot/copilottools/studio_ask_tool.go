package copilottools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/tools"
)

const studioQuestionShown = "pergunta exibida com as opções; a resposta do usuário chega na próxima mensagem"

type studioAskArgs struct {
	Question string   `json:"question" req:"true" desc:"uma pergunta curta e objetiva sobre o resultado que o usuário quer"`
	Options  []string `json:"options" req:"true" desc:"de 2 a 4 respostas curtas (até 80 caracteres) que o usuário escolhe com um toque; ele também pode escrever outra"`
}

type studioAskTool struct{ studioScope }

func NewStudioAskTool() copilot.Tool { return &studioAskTool{} }

func (t *studioAskTool) Meta() copilot.Meta { return chatReadMeta() }

func (t *studioAskTool) Definition() tools.Definition {
	return definition("studio_ask",
		"Faz uma pergunta ao usuário com 2 a 4 opções que ele toca para responder, e encerra a sua resposta para esperar por ela. "+
			"Use antes de uma edição grande quando o objetivo, a duração, o formato, o tom ou o estilo não estiverem claros, "+
			"ou no fim para oferecer o próximo passo. Uma pergunta por vez.",
		studioAskArgs{})
}

func (t *studioAskTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioAskArgs
	if err := decodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if !cc.View.OnStudio() {
		return studioFailure(studioNotHere)
	}
	card, err := copilot.NewQuestionCard(a.Question, a.Options)
	if err != nil {
		return studioFailure("a pergunta precisa de um texto curto e de 2 a 4 opções curtas e diferentes, sem travessão")
	}
	return copilot.Result{Status: copilot.StatusOK, Data: studioQuestionShown, Card: card, EndTurn: true}
}
