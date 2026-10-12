package copilot_usecase

import (
	"errors"
	"fmt"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

const (
	repeatQuestion   = "Chamei %s três vezes seguidas com os mesmos dados e não avancei. Como sigo?"
	repeatGoOn       = "Pode continuar"
	repeatGoOnDetail = "tento de novo do mesmo jeito"
	repeatStop       = "Pare e me explique"
	repeatStopDetail = "conto o que tentei e onde travou"
	askStepName      = "ask_user"
)

func (s *Service) askAfterRepeat(halt error, history []ai.Message, rec *turnRecorder) bool {
	if !errors.Is(halt, agentloop.ErrRepeatedTurn) {
		return false
	}
	card, err := copilot.NewQuestionCard([]copilot.Question{{
		Text:    fmt.Sprintf(repeatQuestion, repeatedToolName(history)),
		Options: []copilot.QuestionOption{{Label: repeatGoOn, Detail: repeatGoOnDetail}, {Label: repeatStop, Detail: repeatStopDetail}},
	}})
	if err != nil {
		return false
	}
	emitStep(rec.emitFn, toolStep{Name: askStepName, Summary: string(copilot.StatusOK), Ok: true, Card: card})
	return true
}

func repeatedToolName(history []ai.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if calls := history[i].ToolCalls; history[i].Role == ai.RoleAssistant && len(calls) > 0 {
			return calls[0].Name
		}
	}
	return "a mesma ferramenta"
}
