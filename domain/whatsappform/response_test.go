package whatsappform

import (
	"errors"
	"reflect"
	"testing"
)

func everyKindDocument() Document {
	secret := textField("f_secret", "Senha")
	secret.Sensitive = true
	return Document{
		Schema:     DocumentSchema,
		Categories: []Category{CategoryLeadGeneration},
		Screens: []Screen{{Key: "s_one", Title: "Tudo", SubmitLabel: "Enviar", Blocks: []Block{
			fieldBlock(textField("f_name", "Nome")),
			fieldBlock(&Field{Key: "f_age", Kind: FieldNumber, Label: "Idade"}),
			fieldBlock(choiceField("f_plan", FieldRadio, "o_a", "o_b")),
			fieldBlock(choiceField("f_tags", FieldCheckbox, "o_a", "o_b")),
			fieldBlock(&Field{Key: "f_day", Kind: FieldDate, Label: "Dia"}),
			fieldBlock(&Field{Key: "f_stay", Kind: FieldDateRange, Label: "Estadia"}),
			fieldBlock(&Field{Key: "f_ok", Kind: FieldOptIn, Label: "Aceito"}),
			fieldBlock(&Field{Key: "f_doc", Kind: FieldDocument, Label: "Documento", Max: 2}),
			fieldBlock(secret),
		}}},
	}
}

const everyKindResponse = `{
	"flow_token": "vf1_token",
	"f_name": "Ana",
	"f_age": 42,
	"f_plan": "o_b",
	"f_tags": ["o_a", "o_b"],
	"f_day": "2026-10-19",
	"f_stay": {"start-date": "2026-11-01", "end-date": "2026-11-05"},
	"f_ok": true,
	"f_doc": [{"file_name": "rg.pdf", "mime_type": "application/pdf", "sha256": "abc", "id": "media-1"}],
	"f_secret": "1234",
	"extra": "kept"
}`

func TestParsesEveryAnswerKindAgainstTheVersion(t *testing.T) {
	answers, err := ParseResponse(everyKindDocument(), []byte(everyKindResponse))
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	want := map[string]Answer{
		"f_name":   {Key: "f_name", Kind: FieldShortText, Text: "Ana"},
		"f_age":    {Key: "f_age", Kind: FieldNumber, Text: "42"},
		"f_plan":   {Key: "f_plan", Kind: FieldRadio, OptionIDs: []string{"o_b"}},
		"f_tags":   {Key: "f_tags", Kind: FieldCheckbox, OptionIDs: []string{"o_a", "o_b"}},
		"f_day":    {Key: "f_day", Kind: FieldDate, Text: "2026-10-19"},
		"f_stay":   {Key: "f_stay", Kind: FieldDateRange, Range: &DateRange{Start: "2026-11-01", End: "2026-11-05"}},
		"f_ok":     {Key: "f_ok", Kind: FieldOptIn, Checked: &yes},
		"f_doc":    {Key: "f_doc", Kind: FieldDocument, Files: []File{{ID: "media-1", Name: "rg.pdf", MimeType: "application/pdf", SHA256: "abc"}}},
		"f_secret": {Key: "f_secret", Kind: FieldShortText, Text: "1234"},
	}
	if answers.Token != "vf1_token" {
		t.Fatalf("token = %q", answers.Token)
	}
	if !reflect.DeepEqual(answers.Values, want) {
		t.Fatalf("values = %+v\nwant     %+v", answers.Values, want)
	}
	if string(answers.Unknown["extra"]) != `"kept"` {
		t.Fatalf("unknown keys are kept raw, got %+v", answers.Unknown)
	}
	if len(answers.Problems) != 0 {
		t.Fatalf("problems = %+v", answers.Problems)
	}
}

func TestAnswersThatDoNotFitTheirFieldAreProblemsNotValues(t *testing.T) {
	raw := `{
		"f_plan": "o_z",
		"f_tags": "o_a",
		"f_day": "19/10/2026",
		"f_ok": "yes",
		"f_doc": [{"id": "1"}, {"id": "2"}, {"id": "3"}],
		"f_name": ["Ana"]
	}`
	answers, err := ParseResponse(everyKindDocument(), []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(answers.Values) != 0 {
		t.Fatalf("no value may be kept from a malformed answer, got %+v", answers.Values)
	}
	want := map[string]string{
		"f_plan": AnswerUnknownOption, "f_tags": AnswerWrongType, "f_day": AnswerInvalidDate,
		"f_ok": AnswerWrongType, "f_doc": AnswerTooManyFiles, "f_name": AnswerWrongType,
	}
	got := map[string]string{}
	for _, p := range answers.Problems {
		got[p.Key] = p.Code
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems = %+v, want %+v", got, want)
	}
}

func TestEmptyAnswersAreSkipped(t *testing.T) {
	answers, err := ParseResponse(everyKindDocument(), []byte(`{"f_name": "  ", "f_tags": [], "f_doc": [], "f_plan": ""}`))
	if err != nil || len(answers.Values) != 0 || len(answers.Problems) != 0 {
		t.Fatalf("empty answers are not answers: %+v, %v", answers, err)
	}
}

func TestAResponseThatIsNotAnObjectIsRefused(t *testing.T) {
	for _, raw := range []string{``, `[]`, `"x"`, `{`} {
		if _, err := ParseResponse(everyKindDocument(), []byte(raw)); !errors.Is(err, ErrResponseInvalid) {
			t.Errorf("ParseResponse(%q) = %v, want ErrResponseInvalid", raw, err)
		}
	}
}

func TestReadTokenWithoutKnowingTheForm(t *testing.T) {
	cases := map[string]string{
		`{"flow_token": "vf1_abc", "x": 1}`: "vf1_abc",
		`{"x": 1}`:                          "",
		`{"flow_token": 7}`:                 "",
	}
	for raw, want := range cases {
		got, err := ReadToken([]byte(raw))
		if err != nil || got != want {
			t.Errorf("ReadToken(%s) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := ReadToken([]byte(`nope`)); !errors.Is(err, ErrResponseInvalid) {
		t.Fatal("a response that is not JSON is refused")
	}
}

func TestTextSummaryUsesQuestionsInOrderAndMasksSensitiveAnswers(t *testing.T) {
	doc := everyKindDocument()
	doc.Screens[0].Blocks[0].Field.Question = "Como podemos te chamar?"
	answers, _ := ParseResponse(doc, []byte(everyKindResponse))
	got := TextSummary(doc, "Cadastro", answers, false)
	want := "Formulário \"Cadastro\" respondido\n" +
		"Como podemos te chamar?: Ana\n" +
		"Idade: 42\n" +
		"Escolha: Opção o_b\n" +
		"Escolha: Opção o_a, Opção o_b\n" +
		"Dia: 19/10/2026\n" +
		"Estadia: 01/11/2026 a 05/11/2026\n" +
		"Aceito: Sim\n" +
		"Documento: ••••\n" +
		"Senha: ••••"
	if got != want {
		t.Fatalf("summary =\n%s\nwant\n%s", got, want)
	}
	revealed := TextSummary(doc, "Cadastro", answers, true)
	if want := "Documento: rg.pdf\nSenha: 1234"; revealed[len(revealed)-len(want):] != want {
		t.Fatalf("revealed summary ends with\n%s", revealed)
	}
}

func TestDisplayValueOfEachAnswer(t *testing.T) {
	doc := everyKindDocument()
	answers, _ := ParseResponse(doc, []byte(everyKindResponse))
	plan, _ := doc.Field("f_plan")
	if got := answers.Values["f_plan"].Display(plan.Field); got != "Opção o_b" {
		t.Fatalf("display = %q", got)
	}
	no := false
	ok, _ := doc.Field("f_ok")
	if got := (Answer{Kind: FieldOptIn, Checked: &no}).Display(ok.Field); got != "Não" {
		t.Fatalf("display = %q", got)
	}
}

func TestADateRangeShowsEachEnd(t *testing.T) {
	start, end := DateRange{Start: "2026-10-01", End: "2026-10-05"}.Shown()
	if start != "01/10/2026" || end != "05/10/2026" {
		t.Fatalf("range = %q %q", start, end)
	}
}
