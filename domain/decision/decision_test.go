package decision

import (
	"errors"
	"strings"
	"testing"
)

func stageQuestion() Question {
	return Choice("Em qual etapa a conversa está?",
		Option{Key: "novo", Description: "Contato novo"},
		Option{Key: "negociacao", Description: "Discute preço"},
	)
}

func TestQuestionsRejectShapesTheModelCannotAnswer(t *testing.T) {
	tooMany := make([]Option, MaxChoiceOptions+1)
	for i := range tooMany {
		tooMany[i] = Option{Key: strings.Repeat("k", i+1), Description: "d"}
	}
	tooManyLevels := make([]string, MaxScoreLevels+1)
	for i := range tooManyLevels {
		tooManyLevels[i] = "nivel"
	}
	cases := map[string]Question{
		"no instructions":        Choice("", Option{Key: "a", Description: "a"}, Option{Key: "b", Description: "b"}),
		"one option":             Choice("q", Option{Key: "a", Description: "a"}),
		"duplicated option":      Choice("q", Option{Key: "a", Description: "a"}, Option{Key: "a", Description: "b"}),
		"blank option key":       Choice("q", Option{Key: " ", Description: "a"}, Option{Key: "b", Description: "b"}),
		"option without meaning": Choice("q", Option{Key: "a"}, Option{Key: "b", Description: "b"}),
		"too many options":       Choice("q", tooMany...),
		"yes without meaning":    YesNo("q", "", "não"),
		"one level":              Score("q", "baixo"),
		"too many levels":        Score("q", tooManyLevels...),
		"unknown kind":           {Kind: "free_text", Instructions: "q"},
	}
	for name, question := range cases {
		t.Run(name, func(t *testing.T) {
			if err := question.Validate(); !errors.Is(err, ErrInvalidQuestion) {
				t.Fatalf("Validate() = %v, want ErrInvalidQuestion", err)
			}
		})
	}
	valid := []Question{stageQuestion(), YesNo("q", "sim", "não"), Score("q", "baixo", "alto")}
	for _, question := range valid {
		if err := question.Validate(); err != nil {
			t.Fatalf("valid %s question rejected: %v", question.Kind, err)
		}
	}
}

func TestARequestNeedsAWorkspaceAndValidQuestions(t *testing.T) {
	good := Request{WorkspaceID: "ws", State: "conversa", Questions: map[string]Question{"stage": stageQuestion()}}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := map[string]Request{
		"no workspace":      {State: "x", Questions: good.Questions},
		"no questions":      {WorkspaceID: "ws", State: "x"},
		"no state":          {WorkspaceID: "ws", Questions: good.Questions},
		"invalid question":  {WorkspaceID: "ws", State: "x", Questions: map[string]Question{"q": YesNo("", "", "")}},
		"blank question id": {WorkspaceID: "ws", State: "x", Questions: map[string]Question{" ": stageQuestion()}},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			if err := request.Validate(); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Validate() = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestAResultMustAnswerEveryQuestionWithinItsOptions(t *testing.T) {
	request := Request{WorkspaceID: "ws", State: "x", Questions: map[string]Question{
		"stage":   stageQuestion(),
		"handoff": YesNo("pediu humano?", "sim", "não"),
		"quality": Score("qualidade", "baixa", "média", "alta"),
	}}
	complete := Result{Answers: map[string]Answer{
		"stage":   {Kind: KindChoice, Choice: "novo", Confidence: 0.9},
		"handoff": {Kind: KindYesNo, Yes: 0.1},
		"quality": {Kind: KindScore, Score: 1.4, Confidence: 0.7},
	}}
	if err := request.Accepts(complete); err != nil {
		t.Fatalf("complete result rejected: %v", err)
	}
	broken := map[string]map[string]Answer{
		"missing answer":      {"stage": complete.Answers["stage"], "handoff": complete.Answers["handoff"]},
		"choice outside set":  {"stage": {Kind: KindChoice, Choice: "fechado", Confidence: 1}, "handoff": complete.Answers["handoff"], "quality": complete.Answers["quality"]},
		"kind mismatch":       {"stage": {Kind: KindYesNo, Yes: 1}, "handoff": complete.Answers["handoff"], "quality": complete.Answers["quality"]},
		"probability above 1": {"stage": complete.Answers["stage"], "handoff": {Kind: KindYesNo, Yes: 1.2}, "quality": complete.Answers["quality"]},
		"confidence below 0":  {"stage": {Kind: KindChoice, Choice: "novo", Confidence: -0.1}, "handoff": complete.Answers["handoff"], "quality": complete.Answers["quality"]},
		"score beyond levels": {"stage": complete.Answers["stage"], "handoff": complete.Answers["handoff"], "quality": {Kind: KindScore, Score: 3, Confidence: 1}},
	}
	for name, answers := range broken {
		t.Run(name, func(t *testing.T) {
			if err := request.Accepts(Result{Answers: answers}); !errors.Is(err, ErrInvalidAnswer) {
				t.Fatalf("Accepts() = %v, want ErrInvalidAnswer", err)
			}
		})
	}
}

func TestAnswersOnlyCountAboveTheirThreshold(t *testing.T) {
	sure := Answer{Kind: KindChoice, Choice: "negociacao", Confidence: 0.85}
	if key, ok := sure.Chosen(0.8); !ok || key != "negociacao" {
		t.Fatalf("Chosen(0.8) = %q, %v", key, ok)
	}
	if _, ok := sure.Chosen(0.9); ok {
		t.Fatal("a choice below the threshold must not count")
	}
	if _, ok := (Answer{Kind: KindYesNo, Yes: 0.99}).Chosen(0.5); ok {
		t.Fatal("a yes/no answer is not a choice")
	}

	yes := Answer{Kind: KindYesNo, Yes: 0.92}
	if !yes.Affirmed(0.9) || yes.Denied(0.9) {
		t.Fatal("0.92 affirms at 0.9 and does not deny")
	}
	no := Answer{Kind: KindYesNo, Yes: 0.05}
	if no.Affirmed(0.5) || !no.Denied(0.9) {
		t.Fatal("0.05 denies at 0.9")
	}
	unsure := Answer{Kind: KindYesNo, Yes: 0.5}
	if unsure.Affirmed(0.6) || unsure.Denied(0.6) {
		t.Fatal("0.5 is neither yes nor no at 0.6")
	}
	if (Answer{Kind: KindChoice, Choice: "x", Confidence: 1}).Affirmed(0.1) {
		t.Fatal("a choice is not a yes")
	}
}

func TestCertaintyIsComparableAcrossKinds(t *testing.T) {
	cases := []struct {
		answer Answer
		want   float64
	}{
		{Answer{Kind: KindChoice, Confidence: 0.7}, 0.7},
		{Answer{Kind: KindScore, Confidence: 0.4}, 0.4},
		{Answer{Kind: KindYesNo, Yes: 0.2}, 0.8},
		{Answer{Kind: KindYesNo, Yes: 0.9}, 0.9},
	}
	for _, c := range cases {
		if got := c.answer.Certainty(); got < c.want-1e-9 || got > c.want+1e-9 {
			t.Fatalf("Certainty(%+v) = %v, want %v", c.answer, got, c.want)
		}
	}
}

func TestThePolicyNeverAllowsAnActionItDoesNotKnow(t *testing.T) {
	policy := Policy{ActionMoveStage: 0.8}
	if !policy.Allows(ActionMoveStage, 0.8) {
		t.Fatal("exactly the threshold allows")
	}
	if policy.Allows(ActionMoveStage, 0.79) {
		t.Fatal("below the threshold never allows")
	}
	if policy.Allows(ActionSkipDealReview, 1) {
		t.Fatal("an action without a threshold must never be allowed")
	}
}

func TestTheDefaultPolicyCoversEveryAction(t *testing.T) {
	policy := DefaultPolicy()
	for _, action := range Actions() {
		threshold, ok := policy[action]
		if !ok || threshold <= 0.5 || threshold > 1 {
			t.Fatalf("default threshold for %s = %v, want a value in (0.5, 1]", action, threshold)
		}
	}
}
