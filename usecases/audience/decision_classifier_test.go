package audience_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	ca "vozko/domain/audience"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
)

type liveSwitch bool

func (l liveSwitch) Acts(context.Context, string) bool { return bool(l) }

type answeringModel struct {
	mu       sync.Mutex
	requests []decision.Request
	answers  func(decision.Request) map[string]decision.Answer
	failOn   int
}

func (m *answeringModel) Decide(_ context.Context, request decision.Request) (decision.Result, error) {
	m.mu.Lock()
	m.requests = append(m.requests, request)
	call := len(m.requests)
	m.mu.Unlock()
	if m.failOn > 0 && call == m.failOn {
		return decision.Result{}, decision.ErrUnavailable
	}
	return decision.Result{Model: "typesafe/jev-1.13", Answers: m.answers(request)}, nil
}

func conversationLabels(decision.Request) map[string]decision.Answer {
	return map[string]decision.Answer{
		ca.FieldInterest:                {Kind: decision.KindChoice, Choice: string(ca.InterestInterested), Confidence: 0.9},
		ca.FieldDisposition:             {Kind: decision.KindChoice, Choice: string(ca.DispositionFillingInfo), Confidence: 0.9},
		ca.FieldSentiment:               {Kind: decision.KindChoice, Choice: string(shared.SentimentPositive), Confidence: 0.9},
		ca.FieldQualification:           {Kind: decision.KindChoice, Choice: string(ca.QualificationHotLead), Confidence: 0.9},
		ca.FieldNextAction:              {Kind: decision.KindChoice, Choice: string(ca.NextActionContinue), Confidence: 0.9},
		ca.QualityKeyGoalProgress:       {Kind: decision.KindScore, Score: 3, Confidence: 0.9},
		ca.QualityKeyCustomerEngagement: {Kind: decision.KindScore, Score: 3, Confidence: 0.9},
		ca.QualityKeyAgentConduct:       {Kind: decision.KindScore, Score: 2, Confidence: 0.9},
		ca.QualityKeyProfessionalism:    {Kind: decision.KindScore, Score: 2, Confidence: 0.9},
		ca.FieldLanguage:                {Kind: decision.KindChoice, Choice: "pt", Confidence: 0.9},
	}
}

func commentLabels(decision.Request) map[string]decision.Answer {
	return map[string]decision.Answer{
		ca.FieldSentiment:            {Kind: decision.KindChoice, Choice: string(shared.SentimentPositive), Confidence: 0.9},
		ca.FieldStance:               {Kind: decision.KindChoice, Choice: string(ca.StanceSupporter), Confidence: 0.9},
		ca.FieldIntent:               {Kind: decision.KindChoice, Choice: string(ca.IntentPraise), Confidence: 0.9},
		ca.FieldTopicKey:             {Kind: decision.KindChoice, Choice: "saude", Confidence: 0.9},
		ca.FieldIsSpam:               {Kind: decision.KindYesNo, Yes: 0.05},
		ca.SeverityKeyToxicity:       {Kind: decision.KindScore, Score: 0, Confidence: 0.9},
		ca.SeverityKeyPersonalAttack: {Kind: decision.KindScore, Score: 0, Confidence: 0.9},
		ca.SeverityKeyLegalRisk:      {Kind: decision.KindScore, Score: 0, Confidence: 0.9},
		ca.FieldLanguage:             {Kind: decision.KindChoice, Choice: "pt", Confidence: 0.9},
	}
}

func conversationRequest(n int) ca.ClassifyRequest {
	items := make([]ca.Item, n)
	for i := range items {
		items[i] = ca.Item{ID: "conv-" + itoa(i+1), Text: "cliente: quero agendar uma consulta " + itoa(i+1)}
	}
	plans, _ := ca.PlanBatches(items, 100, ca.DefaultBudget(), ca.SubjectKindConversation)
	return ca.ClassifyRequest{
		WorkspaceID: "ws-1", SubjectKind: ca.SubjectKindConversation, Model: "anthropic/claude-sonnet",
		Context: ca.ContainerContext{Caption: "Agendar consultas"}, Batch: plans[0],
	}
}

func summaries(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
	res := &ca.ClassifyResult{FinishReason: "stop", Model: req.Model, PromptTokens: 50, CompletionTokens: 20}
	for _, it := range req.Batch.Items {
		res.Results = append(res.Results, ca.BatchResult{Ref: it.Ref, Summary: "Cliente quer agendar.", ProductInterest: "consulta"})
	}
	return res, nil
}

func TestAWorkspaceNotActingLiveKeepsTheFullLLMClassification(t *testing.T) {
	llm := &fakeClassifier{}
	model := &answeringModel{answers: conversationLabels}
	classifier := NewDecisionClassifier(llm, model, liveSwitch(false), "openai/gpt-4o-mini")

	if _, err := classifier.Classify(context.Background(), conversationRequest(2)); err != nil {
		t.Fatal(err)
	}
	if len(model.requests) != 0 || len(llm.Calls) != 1 || llm.Calls[0].SummaryOnly {
		t.Fatalf("decisions %d, llm calls %+v", len(model.requests), llm.Calls)
	}
}

func TestWithoutADecisionModelTheLLMClassifierIsUsedAsIs(t *testing.T) {
	llm := &fakeClassifier{}
	if NewDecisionClassifier(llm, nil, liveSwitch(true), "m") != ca.Classifier(llm) {
		t.Fatal("no decision model means no decorator")
	}
}

func TestALiveConversationTakesLabelsFromDecisionsAndOnlyTheSummaryFromTheLLM(t *testing.T) {
	llm := &fakeClassifier{}
	llm.push(summaries)
	model := &answeringModel{answers: conversationLabels}
	classifier := NewDecisionClassifier(llm, model, liveSwitch(true), "openai/gpt-4o-mini")

	res, err := classifier.Classify(context.Background(), conversationRequest(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(model.requests) != 3 {
		t.Fatalf("one decision per conversation, got %d", len(model.requests))
	}
	for _, request := range model.requests {
		if request.WorkspaceID != "ws-1" || request.Purpose != ld.PurposeAnalysis || request.ReferenceID == "" {
			t.Fatalf("decision request = %+v", request)
		}
	}
	if len(llm.Calls) != 1 || !llm.Calls[0].SummaryOnly || llm.Calls[0].Model != "openai/gpt-4o-mini" {
		t.Fatalf("llm call = %+v", llm.Calls)
	}
	if len(res.Results) != 3 {
		t.Fatalf("results = %+v", res.Results)
	}
	for _, result := range res.Results {
		c := result.ClassificationFor(ca.SubjectKindConversation)
		if c.Qualification != ca.QualificationHotLead || c.Summary != "Cliente quer agendar." || c.ProductInterest != "consulta" {
			t.Fatalf("merged result = %+v", result)
		}
		if err := c.ValidateConversation(); err != nil {
			t.Fatalf("merged result is not a valid classification: %v", err)
		}
	}
	if res.PromptTokens != 50 || res.CompletionTokens != 20 {
		t.Fatalf("usage of the summary call must pass through: %+v", res)
	}
}

func TestTheWorkspaceModelSummarisesWhenNoSummaryModelIsConfigured(t *testing.T) {
	llm := &fakeClassifier{}
	llm.push(summaries)
	classifier := NewDecisionClassifier(llm, &answeringModel{answers: conversationLabels}, liveSwitch(true), "")
	if _, err := classifier.Classify(context.Background(), conversationRequest(1)); err != nil {
		t.Fatal(err)
	}
	if llm.Calls[0].Model != "anthropic/claude-sonnet" {
		t.Fatalf("model = %q", llm.Calls[0].Model)
	}
}

func TestASummaryForAnUnknownConversationIsDropped(t *testing.T) {
	llm := &fakeClassifier{}
	llm.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		res, _ := summaries(req)
		res.Results = append(res.Results, ca.BatchResult{Ref: 99, Summary: "inventada"})
		return res, nil
	})
	classifier := NewDecisionClassifier(llm, &answeringModel{answers: conversationLabels}, liveSwitch(true), "m")
	res, err := classifier.Classify(context.Background(), conversationRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("an invented ref must never become a result: %+v", res.Results)
	}
}

func TestAnyDecisionFailureFallsBackToTheFullLLMClassification(t *testing.T) {
	llm := &fakeClassifier{}
	model := &answeringModel{answers: conversationLabels, failOn: 2}
	classifier := NewDecisionClassifier(llm, model, liveSwitch(true), "m")

	res, err := classifier.Classify(context.Background(), conversationRequest(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.Calls) != 1 || llm.Calls[0].SummaryOnly || llm.Calls[0].Model != "anthropic/claude-sonnet" {
		t.Fatalf("fallback must be the unchanged full request: %+v", llm.Calls)
	}
	if res.Model != "test-model" {
		t.Fatalf("result must come from the llm: %+v", res)
	}
}

func TestAnInvalidDecisionAnswerFallsBackToTheLLM(t *testing.T) {
	llm := &fakeClassifier{}
	model := &answeringModel{answers: func(r decision.Request) map[string]decision.Answer {
		answers := conversationLabels(r)
		delete(answers, ca.FieldInterest)
		return answers
	}}
	if _, err := NewDecisionClassifier(llm, model, liveSwitch(true), "m").Classify(context.Background(), conversationRequest(1)); err != nil {
		t.Fatal(err)
	}
	if len(llm.Calls) != 1 || llm.Calls[0].SummaryOnly {
		t.Fatalf("llm calls = %+v", llm.Calls)
	}
}

func TestASummaryFailureIsReportedLikeAnyClassifierFailure(t *testing.T) {
	llm := &fakeClassifier{}
	boom := errors.New("provider down")
	llm.push(func(ca.ClassifyRequest) (*ca.ClassifyResult, error) { return nil, boom })
	_, err := NewDecisionClassifier(llm, &answeringModel{answers: conversationLabels}, liveSwitch(true), "m").
		Classify(context.Background(), conversationRequest(1))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestLiveCommentsAreClassifiedByDecisionsAlone(t *testing.T) {
	llm := &fakeClassifier{}
	model := &answeringModel{answers: commentLabels}
	request := sampleRequest(2)

	res, err := NewDecisionClassifier(llm, model, liveSwitch(true), "m").Classify(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.Calls) != 0 {
		t.Fatalf("comments need no llm call: %+v", llm.Calls)
	}
	if len(res.Results) != 2 || res.Model != "typesafe/jev-1.13" || res.FinishReason != "stop" {
		t.Fatalf("result = %+v", res)
	}
	state := model.requests[0].State.(map[string]string)
	if state["publicacao"] != "Asfalto novo" || state["comentario"] == "" {
		t.Fatalf("comment state = %+v", state)
	}
	c := res.Results[0].ClassificationFor(ca.SubjectKindComment)
	if err := c.Validate(request.Topics); err != nil {
		t.Fatalf("comment classification: %v", err)
	}
}
