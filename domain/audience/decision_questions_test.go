package audience

import (
	"errors"
	"testing"

	"vozko/domain/decision"
	"vozko/domain/shared"
)

func conversationAnswers() map[string]decision.Answer {
	return map[string]decision.Answer{
		FieldInterest:                {Kind: decision.KindChoice, Choice: string(InterestInterested), Confidence: 0.91},
		FieldDisposition:             {Kind: decision.KindChoice, Choice: string(DispositionFillingInfo), Confidence: 0.7},
		FieldSentiment:               {Kind: decision.KindChoice, Choice: string(shared.SentimentPositive), Confidence: 0.55},
		FieldQualification:           {Kind: decision.KindChoice, Choice: string(QualificationHotLead), Confidence: 0.95},
		FieldNextAction:              {Kind: decision.KindChoice, Choice: string(NextActionContinue), Confidence: 0.8},
		QualityKeyGoalProgress:       {Kind: decision.KindScore, Score: 2.6, Confidence: 0.8},
		QualityKeyCustomerEngagement: {Kind: decision.KindScore, Score: 3, Confidence: 0.9},
		QualityKeyAgentConduct:       {Kind: decision.KindScore, Score: 1.4, Confidence: 0.6},
		QualityKeyProfessionalism:    {Kind: decision.KindScore, Score: 0.2, Confidence: 0.7},
		FieldLanguage:                {Kind: decision.KindChoice, Choice: "pt", Confidence: 0.99},
	}
}

func TestConversationQuestionsAreValidAndCoverEveryLabel(t *testing.T) {
	questions := ConversationDecisionQuestions()
	request := decision.Request{WorkspaceID: "ws", State: "x", Questions: questions}
	if err := request.Validate(); err != nil {
		t.Fatalf("conversation questions are not a valid request: %v", err)
	}
	for _, field := range ConversationClassificationFields() {
		question, ok := questions[field.Key]
		if !ok || question.Kind != decision.KindChoice || len(question.Options) != len(field.Options) {
			t.Fatalf("label %s is not asked as a choice over its %d options", field.Key, len(field.Options))
		}
	}
	for _, dimension := range ConversationQualityDimensions() {
		question, ok := questions[dimension.Key]
		if !ok || question.Kind != decision.KindScore || len(question.Levels) != len(shared.QualityLevelValues()) {
			t.Fatalf("quality %s is not asked as a score over the quality levels", dimension.Key)
		}
	}
	if request.Accepts(decision.Result{Answers: conversationAnswers()}) != nil {
		t.Fatal("a complete answer set must be accepted")
	}
}

func TestAConversationReadingTurnsAnswersIntoLabelsAndQuality(t *testing.T) {
	reading, err := ConversationReadingFrom(decision.Result{Answers: conversationAnswers()})
	if err != nil {
		t.Fatal(err)
	}
	c := reading.Classification
	if c.Interest != InterestInterested || c.Disposition != DispositionFillingInfo || c.Qualification != QualificationHotLead ||
		c.NextAction != NextActionContinue || c.Sentiment != shared.SentimentPositive || c.Language != "pt" {
		t.Fatalf("labels = %+v", c)
	}
	if c.Quality.GoalProgress != shared.QualityLevelHigh || c.Quality.CustomerEngagement != shared.QualityLevelHigh ||
		c.Quality.AgentConduct != shared.QualityLevelLow || c.Quality.Professionalism != shared.QualityLevelNone {
		t.Fatalf("quality levels = %+v", c.Quality)
	}
	if err := c.ValidateConversation(); err != nil {
		t.Fatalf("a reading must be a valid conversation classification: %v", err)
	}
	if reading.Certainty[FieldQualification] != 0.95 {
		t.Fatalf("certainty = %v", reading.Certainty)
	}
	if !reading.Uncertain(FieldSentiment, 0.6) || reading.Uncertain(FieldQualification, 0.6) {
		t.Fatal("sentiment at 0.55 is uncertain at 0.6, qualification at 0.95 is not")
	}
}

func TestAReadingWithAMissingOrForeignAnswerIsRejected(t *testing.T) {
	missing := conversationAnswers()
	delete(missing, FieldDisposition)
	if _, err := ConversationReadingFrom(decision.Result{Answers: missing}); !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("missing answer: %v", err)
	}
	foreign := conversationAnswers()
	foreign[FieldInterest] = decision.Answer{Kind: decision.KindChoice, Choice: "maybe", Confidence: 1}
	if _, err := ConversationReadingFrom(decision.Result{Answers: foreign}); !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("foreign answer: %v", err)
	}
}

func topics() TopicSet {
	return TopicSet{{Key: "preco", Label: "Preço", Description: "valores"}}.Normalize()
}

func commentAnswers() map[string]decision.Answer {
	return map[string]decision.Answer{
		FieldSentiment:            {Kind: decision.KindChoice, Choice: string(shared.SentimentNegative), Confidence: 0.9},
		FieldStance:               {Kind: decision.KindChoice, Choice: string(StanceHostile), Confidence: 0.8},
		FieldIntent:               {Kind: decision.KindChoice, Choice: string(IntentComplaint), Confidence: 0.85},
		FieldTopicKey:             {Kind: decision.KindChoice, Choice: "preco", Confidence: 0.7},
		FieldIsSpam:               {Kind: decision.KindYesNo, Yes: 0.1},
		SeverityKeyToxicity:       {Kind: decision.KindScore, Score: 3, Confidence: 0.9},
		SeverityKeyPersonalAttack: {Kind: decision.KindScore, Score: 2.2, Confidence: 0.8},
		SeverityKeyLegalRisk:      {Kind: decision.KindScore, Score: 0, Confidence: 0.9},
		FieldLanguage:             {Kind: decision.KindChoice, Choice: "pt", Confidence: 0.99},
	}
}

func TestCommentQuestionsAskEveryCommentLabel(t *testing.T) {
	questions := CommentDecisionQuestions(topics())
	request := decision.Request{WorkspaceID: "ws", State: "x", Questions: questions}
	if err := request.Validate(); err != nil {
		t.Fatalf("comment questions are not a valid request: %v", err)
	}
	if questions[FieldIsSpam].Kind != decision.KindYesNo {
		t.Fatal("spam is a yes/no question")
	}
	topic := questions[FieldTopicKey]
	if len(topic.Options) != 2 {
		t.Fatalf("topic options = %+v, want the workspace topic plus other", topic.Options)
	}
	if request.Accepts(decision.Result{Answers: commentAnswers()}) != nil {
		t.Fatal("a complete comment answer set must be accepted")
	}
}

func TestACommentClassificationComputesSeverityFromTheLevels(t *testing.T) {
	c, err := CommentClassificationFrom(decision.Result{Answers: commentAnswers()}, topics())
	if err != nil {
		t.Fatal(err)
	}
	if c.Sentiment != shared.SentimentNegative || c.Stance != StanceHostile || c.Intent != IntentComplaint ||
		c.TopicKey != "preco" || c.IsSpam || c.Language != "pt" {
		t.Fatalf("labels = %+v", c)
	}
	if c.Toxicity != shared.QualityLevelHigh || c.PersonalAttack != shared.QualityLevelMedium || c.LegalRisk != shared.QualityLevelNone {
		t.Fatalf("severity levels = %s %s %s", c.Toxicity, c.PersonalAttack, c.LegalRisk)
	}
	if c.Severity() == 0 {
		t.Fatal("a hostile, toxic comment must have a severity")
	}
}

func TestAWorkspaceWithoutTopicsFilesEveryCommentUnderOther(t *testing.T) {
	onlyOther := TopicSet{}.Normalize()
	questions := CommentDecisionQuestions(onlyOther)
	if _, asked := questions[FieldTopicKey]; asked {
		t.Fatal("a single topic is not a question")
	}
	answers := commentAnswers()
	delete(answers, FieldTopicKey)
	c, err := CommentClassificationFrom(decision.Result{Answers: answers}, onlyOther)
	if err != nil || c.TopicKey != TopicKeyOther {
		t.Fatalf("topic = %q, err = %v", c.TopicKey, err)
	}
}

func TestSpamIsDecidedAtEvenOdds(t *testing.T) {
	answers := commentAnswers()
	answers[FieldIsSpam] = decision.Answer{Kind: decision.KindYesNo, Yes: 0.64}
	c, err := CommentClassificationFrom(decision.Result{Answers: answers}, topics())
	if err != nil || !c.IsSpam {
		t.Fatalf("spam = %v, err = %v", c.IsSpam, err)
	}
}

func TestAConversationResultRoundTripsThroughAClassification(t *testing.T) {
	reading, err := ConversationReadingFrom(decision.Result{Answers: conversationAnswers()})
	if err != nil {
		t.Fatal(err)
	}
	result := NewBatchResult(7, SubjectKindConversation, reading.Classification)
	if result.Ref != 7 {
		t.Fatalf("ref = %d", result.Ref)
	}
	back := result.ClassificationFor(SubjectKindConversation)
	if back.Qualification != reading.Classification.Qualification || back.Quality != reading.Classification.Quality ||
		back.Interest != reading.Classification.Interest || back.Language != "pt" {
		t.Fatalf("round trip lost labels: %+v", back)
	}
}

func TestACommentResultRoundTripsThroughAClassification(t *testing.T) {
	c, err := CommentClassificationFrom(decision.Result{Answers: commentAnswers()}, topics())
	if err != nil {
		t.Fatal(err)
	}
	back := NewBatchResult(2, SubjectKindComment, c).ClassificationFor(SubjectKindComment)
	if back != c {
		t.Fatalf("round trip = %+v, want %+v", back, c)
	}
}

func TestTheSummaryTextJoinsTheLabelsOfTheSameConversation(t *testing.T) {
	labels := BatchResult{Ref: 3, Qualification: "hot_lead", Summary: "ignored"}
	merged := labels.WithText(BatchResult{Ref: 3, Summary: "Cliente quer o plano anual.", ProductInterest: "plano anual", Qualification: "cold_lead"})
	if merged.Summary != "Cliente quer o plano anual." || merged.ProductInterest != "plano anual" || merged.Qualification != "hot_lead" {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestTheSummarySchemaAsksOnlyForText(t *testing.T) {
	schema := ConversationSummaryResponseSchema()
	items := schema["properties"].(map[string]any)[SchemaKeyResults].(map[string]any)["items"].(map[string]any)
	props := items["properties"].(map[string]any)
	if len(props) != 3 || props[FieldSummary] == nil || props[FieldProductInterest] == nil || props[FieldRef] == nil {
		t.Fatalf("summary schema properties = %v", props)
	}
}

func TestTheAuthorRoleIsAskedOverEveryRole(t *testing.T) {
	questions := AuthorRoleQuestions()
	request := decision.Request{WorkspaceID: "ws", State: "x", Questions: questions}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(questions[FieldRole].Options) != len(AllAuthorRoles()) {
		t.Fatalf("options = %+v", questions[FieldRole].Options)
	}
}

func TestAnAuthorRoleCarriesItsCertaintyAsAConfidenceLevel(t *testing.T) {
	cases := []struct {
		certainty float64
		want      shared.QualityLevel
	}{
		{0.9, shared.QualityLevelHigh},
		{0.85, shared.QualityLevelHigh},
		{0.75, shared.QualityLevelMedium},
		{0.55, shared.QualityLevelLow},
		{0.3, shared.QualityLevelNone},
	}
	for _, c := range cases {
		result := decision.Result{Answers: map[string]decision.Answer{
			FieldRole: {Kind: decision.KindChoice, Choice: string(RoleJournalist), Confidence: c.certainty},
		}}
		role, level, err := AuthorRoleFrom(result)
		if err != nil || role != RoleJournalist || level != c.want {
			t.Fatalf("certainty %v: role %s level %s err %v", c.certainty, role, level, err)
		}
	}
}

func TestAnAuthorRoleOutsideTheListIsRejected(t *testing.T) {
	result := decision.Result{Answers: map[string]decision.Answer{
		FieldRole: {Kind: decision.KindChoice, Choice: "astronaut", Confidence: 0.99},
	}}
	if _, _, err := AuthorRoleFrom(result); !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := AuthorRoleFrom(decision.Result{}); !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("missing answer err = %v", err)
	}
}
