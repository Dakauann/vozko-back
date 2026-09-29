package livedecision

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/audience"
	"vozko/domain/decision"
)

type Features struct {
	Staging  bool
	Analysis bool
}

func (f Features) Any() bool {
	return f.Staging || f.Analysis
}

type Role string

const (
	RoleCustomer Role = "cliente"
	RoleAgent    Role = "atendente"
)

type Turn struct {
	Role Role
	Text string
	At   time.Time
}

type StageOption struct {
	ID          string
	Name        string
	Description string
}

type Snapshot struct {
	WorkspaceID    string
	EntryID        string
	EntryType      string
	Turns          []Turn
	CurrentStageID string
	Stages         []StageOption
	OpenDeals      []string
}

const (
	MaxTurns      = 40
	MaxStateRunes = 20000
)

func (s Snapshot) LastMessageAt() time.Time {
	if len(s.Turns) == 0 {
		return time.Time{}
	}
	return s.Turns[len(s.Turns)-1].At
}

func (s Snapshot) CustomerWrote() bool {
	for _, turn := range s.Turns {
		if turn.Role == RoleCustomer {
			return true
		}
	}
	return false
}

func (s Snapshot) currentStage() (StageOption, bool) {
	for _, stage := range s.Stages {
		if stage.ID == s.CurrentStageID {
			return stage, true
		}
	}
	return StageOption{}, false
}

func (s Snapshot) State() map[string]any {
	state := map[string]any{"conversa": trimTurns(s.Turns)}
	if stage, ok := s.currentStage(); ok {
		state["etapa_atual"] = stage.Name
	}
	if len(s.OpenDeals) > 0 {
		state["oportunidades_abertas"] = s.OpenDeals
	}
	return state
}

func trimTurns(turns []Turn) []map[string]string {
	kept := make([]map[string]string, 0, min(len(turns), MaxTurns))
	budget := MaxStateRunes
	for i := len(turns) - 1; i >= 0 && len(kept) < MaxTurns && budget > 0; i-- {
		text := []rune(turns[i].Text)
		if len(text) > budget {
			if len(kept) > 0 {
				break
			}
			text = text[len(text)-budget:]
		}
		budget -= len(text)
		kept = append(kept, map[string]string{"de": string(turns[i].Role), "texto": string(text)})
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}

const (
	QuestionStage      = "stage"
	QuestionNewMemory  = "new_memory"
	QuestionDealChange = "deal_change"
)

func asksStage(s Snapshot, f Features) bool { return f.Staging && len(s.Stages) >= 2 }

func Questions(s Snapshot, f Features) map[string]decision.Question {
	questions := map[string]decision.Question{}
	if !s.CustomerWrote() {
		return questions
	}
	if asksStage(s, f) {
		options := make([]decision.Option, 0, len(s.Stages))
		for _, stage := range s.Stages {
			description := stage.Name
			if strings.TrimSpace(stage.Description) != "" {
				description += ": " + stage.Description
			}
			options = append(options, decision.Option{Key: stage.ID, Description: description})
		}
		questions[QuestionStage] = decision.Choice(
			"Em qual etapa do funil esta conversa está agora, considerando as mensagens mais recentes da conversa?",
			options...)
	}
	if f.Analysis {
		for id, question := range audience.ConversationDecisionQuestions() {
			questions[id] = question
		}
	}
	return questions
}

type Outcome struct {
	MoveStageTo    string
	MoveCertainty  float64
	StageSettled   bool
	StageUncertain bool
	Reading        *audience.ConversationReading
}

func Interpret(s Snapshot, f Features, result decision.Result, policy decision.Policy) (Outcome, error) {
	var outcome Outcome
	lookup := func(id string) (decision.Answer, error) {
		answer, ok := result.Answer(id)
		if !ok {
			return decision.Answer{}, fmt.Errorf("%w: %q was asked and not answered", decision.ErrInvalidAnswer, id)
		}
		return answer, nil
	}
	if asksStage(s, f) {
		answer, err := lookup(QuestionStage)
		if err != nil {
			return Outcome{}, err
		}
		if !policy.Allows(decision.ActionMoveStage, answer.Certainty()) {
			outcome.StageUncertain = true
		} else {
			outcome.StageSettled = true
			if answer.Choice != s.CurrentStageID {
				outcome.MoveStageTo = answer.Choice
				outcome.MoveCertainty = answer.Certainty()
			}
		}
	}
	if f.Analysis {
		reading, err := audience.ConversationReadingFrom(result)
		if err != nil {
			return Outcome{}, fmt.Errorf("%w: %v", decision.ErrInvalidAnswer, err)
		}
		outcome.Reading = &reading
	}
	return outcome, nil
}

func denied(answer decision.Answer, policy decision.Policy, action decision.Action) bool {
	threshold, ok := policy.Threshold(action)
	return ok && answer.Denied(threshold)
}

const DealChangeNone = "nada"

func QuietQuestions(wantMemory, wantDeals bool) map[string]decision.Question {
	questions := map[string]decision.Question{}
	if wantMemory {
		questions[QuestionNewMemory] = decision.YesNo(
			"A conversa revelou algum fato novo e duradouro sobre o cliente (preferência, combinado, data, objeção, dado pessoal) que ainda não esteja na memória informada no estado?",
			"há um fato novo e duradouro para lembrar",
			"nada novo que valha lembrar")
	}
	if wantDeals {
		questions[QuestionDealChange] = decision.Choice(
			"Considerando a conversa e as oportunidades abertas no estado, o que mudou nas oportunidades de venda?",
			decision.Option{Key: DealChangeNone, Description: "nada mudou nas oportunidades"},
			decision.Option{Key: "criar", Description: "o cliente demonstrou intenção real de compra e não há oportunidade aberta para isso"},
			decision.Option{Key: "atualizar_valor", Description: "um valor foi combinado ou alterado"},
			decision.Option{Key: "mover", Description: "a negociação avançou para outra etapa"},
			decision.Option{Key: "ganhar", Description: "o cliente confirmou a compra ou o pagamento"},
			decision.Option{Key: "perder", Description: "o cliente desistiu da compra"},
		)
	}
	return questions
}

type QuietGate struct {
	NeedsMemory bool
	NeedsDeals  bool
}

func InterpretQuiet(result decision.Result, wantMemory, wantDeals bool, policy decision.Policy) QuietGate {
	gate := QuietGate{}
	if wantMemory {
		answer, ok := result.Answer(QuestionNewMemory)
		gate.NeedsMemory = !ok || !denied(answer, policy, decision.ActionSkipMemoryReview)
	}
	if wantDeals {
		answer, ok := result.Answer(QuestionDealChange)
		threshold, known := policy.Threshold(decision.ActionSkipDealReview)
		choice, sure := answer.Chosen(threshold)
		gate.NeedsDeals = !ok || !known || !sure || choice != DealChangeNone
	}
	return gate
}

const (
	PurposeLive       = "live"
	PurposeQuietGate  = "quiet_gate"
	PurposeAnalysis   = "analysis"
	PurposeComment    = "comment"
	PurposeAuthorRole = "author_role"
)
