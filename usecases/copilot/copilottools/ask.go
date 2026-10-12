package copilottools

import (
	"fmt"

	"vozko/domain/copilot"
)

const QuestionShown = "cartão exibido; a resposta do usuário chega na próxima mensagem (num formulário, as respostas chegam juntas, cada pergunta seguida da resposta escolhida ou escrita)"

var QuestionRefused = fmt.Sprintf("o cartão precisa de 1 a %d perguntas curtas e diferentes, cada uma com %d a %d opções curtas e diferentes (label até %d caracteres, detail até %d), sem travessão",
	copilot.MaxFormQuestions, copilot.MinQuestionOptions, copilot.MaxQuestionOptions, copilot.MaxQuestionOptionRunes, copilot.MaxQuestionDetailRunes)

type AskOption struct {
	Label  string `json:"label" req:"true" desc:"a resposta curta que o usuário toca (até 80 caracteres)"`
	Detail string `json:"detail" desc:"opcional: o que essa escolha significa na peça, em poucas palavras (até 120 caracteres)"`
}

type AskQuestion struct {
	Question string      `json:"question" req:"true" desc:"uma pergunta curta e objetiva"`
	Options  []AskOption `json:"options" req:"true" desc:"de 2 a 4 opções diferentes; o usuário também pode escrever outra resposta"`
}

type AskArgs struct {
	Questions []AskQuestion `json:"questions" req:"true" desc:"de 1 a 4 perguntas; com mais de uma, o cartão vira um formulário curto respondido de uma vez"`
}

func (a AskArgs) questions() []copilot.Question {
	out := make([]copilot.Question, 0, len(a.Questions))
	for _, q := range a.Questions {
		options := make([]copilot.QuestionOption, 0, len(q.Options))
		for _, o := range q.Options {
			options = append(options, copilot.QuestionOption{Label: o.Label, Detail: o.Detail})
		}
		out = append(out, copilot.Question{Text: q.Question, Options: options})
	}
	return out
}

func AskResult(a AskArgs) copilot.Result {
	card, err := copilot.NewQuestionCard(a.questions())
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: QuestionRefused}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: QuestionShown, Card: card, EndTurn: true}
}
