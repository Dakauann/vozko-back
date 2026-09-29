package livedecision

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/audience"
	"vozko/domain/decision"
	"vozko/domain/shared"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func snapshot() Snapshot {
	return Snapshot{
		WorkspaceID: "ws",
		EntryID:     "entry",
		EntryType:   "whatsapp",
		Turns: []Turn{
			{Role: RoleAgent, Text: "O plano anual sai R$ 1.200.", At: t0},
			{Role: RoleCustomer, Text: "fechado, manda o link", At: t0.Add(time.Minute)},
		},
		CurrentStageID: "st-negociacao",
		Stages: []StageOption{
			{ID: "st-negociacao", Name: "Negociação", Description: "discute preço"},
			{ID: "st-fechamento", Name: "Fechamento", Description: "confirmou a compra"},
		},
	}
}

func allFeatures() Features {
	return Features{Staging: true, Analysis: true}
}

func analysisAnswers() map[string]decision.Answer {
	return map[string]decision.Answer{
		audience.FieldInterest:                {Kind: decision.KindChoice, Choice: "interested", Confidence: 0.9},
		audience.FieldDisposition:             {Kind: decision.KindChoice, Choice: "filling_info", Confidence: 0.8},
		audience.FieldSentiment:               {Kind: decision.KindChoice, Choice: "positive", Confidence: 0.9},
		audience.FieldQualification:           {Kind: decision.KindChoice, Choice: "hot_lead", Confidence: 0.97},
		audience.FieldNextAction:              {Kind: decision.KindChoice, Choice: "continue", Confidence: 0.7},
		audience.QualityKeyGoalProgress:       {Kind: decision.KindScore, Score: 3, Confidence: 0.9},
		audience.QualityKeyCustomerEngagement: {Kind: decision.KindScore, Score: 2, Confidence: 0.9},
		audience.QualityKeyAgentConduct:       {Kind: decision.KindScore, Score: 2, Confidence: 0.9},
		audience.QualityKeyProfessionalism:    {Kind: decision.KindScore, Score: 3, Confidence: 0.9},
		audience.FieldLanguage:                {Kind: decision.KindChoice, Choice: "pt", Confidence: 0.99},
	}
}

func answers(extra map[string]decision.Answer) decision.Result {
	all := analysisAnswers()
	for id, answer := range extra {
		all[id] = answer
	}
	return decision.Result{Answers: all}
}

func TestOnlyEnabledFeaturesBecomeQuestions(t *testing.T) {
	questions := Questions(snapshot(), Features{Staging: true})
	if _, ok := questions[QuestionStage]; !ok {
		t.Fatal("staging asks the stage")
	}
	if _, ok := questions[audience.FieldInterest]; ok {
		t.Fatal("analysis was asked without its feature")
	}
	stage := questions[QuestionStage]
	if len(stage.Options) != 2 || stage.Options[0].Key != "st-negociacao" || !strings.Contains(stage.Options[1].Description, "Fechamento") {
		t.Fatalf("stage options = %+v", stage.Options)
	}
	request := decision.Request{WorkspaceID: "ws", State: snapshot().State(), Questions: Questions(snapshot(), allFeatures())}
	if err := request.Validate(); err != nil {
		t.Fatalf("the full question set is not a valid request: %v", err)
	}
}

func TestAPipelineWithOneStageIsNotAQuestion(t *testing.T) {
	s := snapshot()
	s.Stages = s.Stages[:1]
	if _, ok := Questions(s, Features{Staging: true})[QuestionStage]; ok {
		t.Fatal("one stage leaves nothing to choose")
	}
}

func TestTheStateKeepsTheNewestTurnsWithinTheBudget(t *testing.T) {
	s := snapshot()
	s.Turns = nil
	for i := 0; i < 200; i++ {
		s.Turns = append(s.Turns, Turn{Role: RoleCustomer, Text: strings.Repeat("a", 500), At: t0.Add(time.Duration(i) * time.Second)})
	}
	s.Turns[len(s.Turns)-1].Text = "a mensagem mais nova"
	state := s.State()
	conversation := state["conversa"].([]map[string]string)
	if len(conversation) > MaxTurns {
		t.Fatalf("kept %d turns, want at most %d", len(conversation), MaxTurns)
	}
	total := 0
	for _, turn := range conversation {
		total += len([]rune(turn["texto"]))
	}
	if total > MaxStateRunes {
		t.Fatalf("kept %d runes, budget is %d", total, MaxStateRunes)
	}
	if conversation[len(conversation)-1]["texto"] != "a mensagem mais nova" {
		t.Fatal("the newest message must survive trimming")
	}
	if state["etapa_atual"] != "Negociação" {
		t.Fatalf("current stage = %v", state["etapa_atual"])
	}
	if conversation[0]["de"] != string(RoleCustomer) {
		t.Fatalf("role = %v", conversation[0]["de"])
	}
}

func TestAGiantSingleMessageIsCutRatherThanDropped(t *testing.T) {
	s := snapshot()
	s.Turns = []Turn{{Role: RoleCustomer, Text: strings.Repeat("b", MaxStateRunes*2), At: t0}}
	conversation := s.State()["conversa"].([]map[string]string)
	if len(conversation) != 1 || len([]rune(conversation[0]["texto"])) != MaxStateRunes {
		t.Fatalf("a single oversized message must be kept, cut to the budget")
	}
}

func TestTheLastMessageFromAnyoneIsTheWatermark(t *testing.T) {
	s := snapshot()
	s.Turns = append(s.Turns, Turn{Role: RoleAgent, Text: "agendado para sexta", At: t0.Add(2 * time.Minute)})
	if !s.LastMessageAt().Equal(t0.Add(2 * time.Minute)) {
		t.Fatalf("watermark = %v", s.LastMessageAt())
	}
}

func TestAConversationTheCustomerNeverWroteInAsksNothing(t *testing.T) {
	s := snapshot()
	s.Turns = []Turn{{Role: RoleAgent, Text: "Oferta da semana: plano anual com desconto", At: t0}}
	if questions := Questions(s, Features{Staging: true, Analysis: true}); len(questions) != 0 {
		t.Fatalf("questions = %v", questions)
	}
}

func TestTheStageQuestionWeighsTheWholeConversation(t *testing.T) {
	question := Questions(snapshot(), Features{Staging: true})[QuestionStage]
	if strings.Contains(question.Instructions, "cliente") || strings.Contains(question.Instructions, "Se nada mudou") {
		t.Fatalf("instructions = %q", question.Instructions)
	}
}

func TestAConfidentStageChangeMovesAndAnUnsureOneWaits(t *testing.T) {
	policy := decision.DefaultPolicy()
	sure := answers(map[string]decision.Answer{QuestionStage: {Kind: decision.KindChoice, Choice: "st-fechamento", Confidence: 0.92}})
	outcome, err := Interpret(snapshot(), Features{Staging: true, Analysis: true}, sure, policy)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.MoveStageTo != "st-fechamento" || outcome.StageUncertain || !outcome.StageSettled {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.MoveCertainty != sure.Answers[QuestionStage].Certainty() {
		t.Fatalf("a move carries the certainty that allowed it: %v", outcome.MoveCertainty)
	}

	unsure := answers(map[string]decision.Answer{QuestionStage: {Kind: decision.KindChoice, Choice: "st-fechamento", Confidence: 0.6}})
	outcome, _ = Interpret(snapshot(), Features{Staging: true, Analysis: true}, unsure, policy)
	if outcome.MoveStageTo != "" || !outcome.StageUncertain || outcome.StageSettled {
		t.Fatalf("an unsure stage must not move and must be left to the quiet window: %+v", outcome)
	}

	same := answers(map[string]decision.Answer{QuestionStage: {Kind: decision.KindChoice, Choice: "st-negociacao", Confidence: 0.95}})
	outcome, _ = Interpret(snapshot(), Features{Staging: true, Analysis: true}, same, policy)
	if outcome.MoveStageTo != "" || outcome.StageUncertain || !outcome.StageSettled {
		t.Fatalf("keeping the stage is a confident no-op: %+v", outcome)
	}
}

func TestAnalysisBecomesAReading(t *testing.T) {
	outcome, err := Interpret(snapshot(), Features{Analysis: true}, answers(nil), decision.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Reading == nil || outcome.Reading.Classification.Qualification != audience.QualificationHotLead ||
		outcome.Reading.Classification.Sentiment != shared.SentimentPositive {
		t.Fatalf("reading = %+v", outcome.Reading)
	}
	withoutAnalysis, _ := Interpret(snapshot(), Features{}, decision.Result{}, decision.DefaultPolicy())
	if withoutAnalysis.Reading != nil {
		t.Fatal("no analysis feature, no reading")
	}
}

func TestAMissingAnswerForAnAskedQuestionIsAnError(t *testing.T) {
	_, err := Interpret(snapshot(), Features{Staging: true, Analysis: true}, answers(nil), decision.DefaultPolicy())
	if !errors.Is(err, decision.ErrInvalidAnswer) {
		t.Fatalf("Interpret() = %v, want ErrInvalidAnswer", err)
	}
}

func TestTheQuietGateCallsTheLLMOnlyWhenSomethingChanged(t *testing.T) {
	policy := decision.DefaultPolicy()
	questions := QuietQuestions(true, true)
	if questions[QuestionNewMemory].Kind != decision.KindYesNo || questions[QuestionDealChange].Kind != decision.KindChoice {
		t.Fatalf("quiet questions = %+v", questions)
	}
	nothing := decision.Result{Answers: map[string]decision.Answer{
		QuestionNewMemory:  {Kind: decision.KindYesNo, Yes: 0.1},
		QuestionDealChange: {Kind: decision.KindChoice, Choice: DealChangeNone, Confidence: 0.9},
	}}
	gate := InterpretQuiet(nothing, true, true, policy)
	if gate.NeedsMemory || gate.NeedsDeals {
		t.Fatalf("nothing changed, nothing to review: %+v", gate)
	}
	something := decision.Result{Answers: map[string]decision.Answer{
		QuestionNewMemory:  {Kind: decision.KindYesNo, Yes: 0.3},
		QuestionDealChange: {Kind: decision.KindChoice, Choice: "ganhar", Confidence: 0.6},
	}}
	gate = InterpretQuiet(something, true, true, policy)
	if !gate.NeedsMemory || !gate.NeedsDeals {
		t.Fatalf("anything short of a confident no goes to the LLM: %+v", gate)
	}
	unsureNothing := decision.Result{Answers: map[string]decision.Answer{
		QuestionDealChange: {Kind: decision.KindChoice, Choice: DealChangeNone, Confidence: 0.5},
	}}
	if !InterpretQuiet(unsureNothing, false, true, policy).NeedsDeals {
		t.Fatal("an unsure 'nothing' still goes to the LLM")
	}
	if InterpretQuiet(decision.Result{}, false, false, policy) != (QuietGate{}) {
		t.Fatal("nothing asked, nothing needed")
	}
	if len(QuietQuestions(false, false)) != 0 {
		t.Fatal("no gate questions when neither review is wanted")
	}
	if !InterpretQuiet(decision.Result{}, true, true, policy).NeedsMemory {
		t.Fatal("a missing gate answer must fall back to the LLM, never skip it")
	}
}
