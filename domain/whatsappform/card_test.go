package whatsappform

import (
	"reflect"
	"testing"
)

func TestResponseCardMasksSensitiveAnswersAndCarriesFiles(t *testing.T) {
	doc := everyKindDocument()
	answers, _ := ParseResponse(doc, []byte(everyKindResponse))
	resp := Response{ID: "resp-1", FormID: "form-1", Match: MatchByToken, Answers: answers,
		Report: ApplyReport{Changed: []string{"email"}, Conflicts: []string{"name"}, Files: []FileOutcome{{Key: "f_doc", FileID: "media-1", MediaID: "m-9"}}}}
	card := NewResponseCard(&doc, "Cadastro", resp)
	if card.Kind != CardResponse || card.ResponseID != "resp-1" || card.FormName != "Cadastro" || card.Match != MatchByToken {
		t.Fatalf("card = %+v", card)
	}
	if !reflect.DeepEqual(card.Changed, []string{"email"}) || !reflect.DeepEqual(card.Conflicts, []string{"name"}) {
		t.Fatalf("report = %+v %+v", card.Changed, card.Conflicts)
	}
	byKey := map[string]CardAnswer{}
	for _, a := range card.Answers {
		byKey[a.Key] = a
	}
	if byKey["f_name"].Value != "Ana" || byKey["f_secret"].Value != maskedAnswer || !byKey["f_secret"].Sensitive {
		t.Fatalf("answers = %+v", card.Answers)
	}
	doc0 := byKey["f_doc"]
	if doc0.Value != maskedAnswer || len(doc0.Files) != 1 || doc0.Files[0].MediaID != "m-9" || doc0.Files[0].Name != "" {
		t.Fatalf("a sensitive file shows only that it exists, got %+v", doc0)
	}
	if card.Answers[0].Key != "f_name" {
		t.Fatal("answers follow the order of the form")
	}
}

func TestAnUnmatchedResponseCardShowsRawKeys(t *testing.T) {
	answers, _ := ParseResponse(Document{}, []byte(`{"name":"Ana","age":3}`))
	card := NewResponseCard(nil, "", Response{ID: "resp-2", Match: UnmatchedNoToken, Answers: answers})
	if len(card.Answers) != 2 || card.Answers[0].Key != "age" || card.Answers[0].Value != "3" || card.Answers[1].Prompt != "name" {
		t.Fatalf("card = %+v", card.Answers)
	}
}

func TestResponseSummaryForUnmatchedAnswers(t *testing.T) {
	answers, _ := ParseResponse(Document{}, []byte(`{"name":"Ana"}`))
	if got := UnmatchedSummary(answers); got != "Formulário respondido\nname: Ana" {
		t.Fatalf("summary = %q", got)
	}
}

func TestMaskSensitiveHidesEveryShapeOfASensitiveAnswer(t *testing.T) {
	doc := everyKindDocument()
	answers, _ := ParseResponse(doc, []byte(everyKindResponse))
	masked := MaskSensitive(doc, answers)
	secret, file := masked.Values["f_secret"], masked.Values["f_doc"]
	if !secret.Masked || secret.Text != maskedAnswer || !file.Masked || file.Files[0].Name != "" || file.Files[0].ID != "media-1" {
		t.Fatalf("masked = %+v %+v", secret, file)
	}
	if name := masked.Values["f_name"]; name.Masked || name.Text != "Ana" {
		t.Fatalf("a regular answer stays, got %+v", name)
	}
	if answers.Values["f_secret"].Text != "1234" {
		t.Fatal("masking returns a copy and leaves the stored answers alone")
	}
}

func TestUnknownLinesListRawAnswersByKey(t *testing.T) {
	answers, _ := ParseResponse(Document{}, []byte(`{"name":"Ana","age":3}`))
	if got := answers.UnknownLines(); !reflect.DeepEqual(got, []string{"age: 3", "name: Ana"}) {
		t.Fatalf("lines = %v", got)
	}
	if got := (Answers{}).UnknownLines(); len(got) != 0 {
		t.Fatalf("no raw answers gives no lines, got %v", got)
	}
}
