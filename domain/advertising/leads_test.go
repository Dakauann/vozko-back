package advertising

import (
	"strings"
	"testing"
)

func validForm() LeadFormDraft {
	d := LeadFormDraft{
		AdAccountID: "a", PageID: "p", Name: "Orçamento", PrivacyURL: "https://loja.example.com/privacidade",
		Questions:     []FormQuestion{{Type: QuestionFullName}, {Type: QuestionPhone}, {Type: QuestionCustom, Label: "Qual o melhor horário?", Options: []string{"Manhã", "Tarde", " "}}},
		ThankYouTitle: "Obrigado", ThankYouURL: "https://loja.example.com", ThankYouButtonText: "Visitar site",
	}
	d.Normalize()
	return d
}

func TestLeadFormNeedsAThankYouPageWithAWebsite(t *testing.T) {
	d := validForm()
	d.ThankYouTitle, d.ThankYouURL, d.ThankYouButtonText = " ", "", " "
	requireIssues(t, d.Validate(), FieldIssue{"thankYouTitle", "required"}, FieldIssue{"thankYouUrl", "required"}, FieldIssue{"thankYouButtonText", "required"})
	d = validForm()
	d.ThankYouButtonText = strings.Repeat("a", 61)
	requireIssues(t, d.Validate(), FieldIssue{"thankYouButtonText", "too_long"})
	d = validForm()
	d.ThankYouURL = "http://loja.example.com"
	requireIssues(t, d.Validate(), FieldIssue{"thankYouUrl", "invalid_url"})
}

func TestPrivacyLinkTextFollowsMetasLimit(t *testing.T) {
	d := validForm()
	d.PrivacyText = strings.Repeat("a", 70)
	if err := d.Validate(); err != nil {
		t.Fatalf("form refused: %v", err)
	}
	d.PrivacyText += "a"
	requireIssues(t, d.Validate(), FieldIssue{"privacyText", "too_long"})
}

func TestLeadFormLocaleIsMetasEnum(t *testing.T) {
	d := validForm()
	if d.Locale != "PT_BR" {
		t.Fatalf("default locale %q", d.Locale)
	}
	d.Locale = " en_us "
	d.Normalize()
	if err := d.Validate(); err != nil || d.Locale != "EN_US" {
		t.Fatalf("locale %q refused: %v", d.Locale, err)
	}
	d.Locale = "XX_YY"
	requireIssues(t, d.Validate(), FieldIssue{"locale", "invalid"})
}

func TestIntroIsOptional(t *testing.T) {
	d := validForm()
	d.Intro = nil
	if err := d.Validate(); err != nil {
		t.Fatalf("form refused: %v", err)
	}
}

func TestIntroCleansItsContent(t *testing.T) {
	d := validForm()
	d.Intro = &FormIntro{Title: " Fale com a gente ", Style: IntroList, Content: []string{" Rápido ", " ", "Sem custo"}}
	d.Normalize()
	if err := d.Validate(); err != nil {
		t.Fatalf("form refused: %v", err)
	}
	if d.Intro.Title != "Fale com a gente" || strings.Join(d.Intro.Content, "|") != "Rápido|Sem custo" {
		t.Fatalf("intro %+v", d.Intro)
	}
}

func TestIntroNeedsTitleStyleAndContent(t *testing.T) {
	d := validForm()
	d.Intro = &FormIntro{Style: "BULLETS"}
	requireIssues(t, d.Validate(), FieldIssue{"intro.title", "required"}, FieldIssue{"intro.style", "invalid"}, FieldIssue{"intro.content", "required"})
}

func TestIntroLimits(t *testing.T) {
	d := validForm()
	d.Intro = &FormIntro{Title: strings.Repeat("t", 61), Style: IntroList, Content: []string{"1", "2", "3", "4", "5", "6"}}
	requireIssues(t, d.Validate(), FieldIssue{"intro.title", "too_long"}, FieldIssue{"intro.content", "too_many"})
	d.Intro = &FormIntro{Title: strings.Repeat("t", 60), Style: IntroList, Content: []string{strings.Repeat("c", 80)}}
	if err := d.Validate(); err != nil {
		t.Fatalf("form refused: %v", err)
	}
	d.Intro.Content = []string{strings.Repeat("c", 81)}
	requireIssues(t, d.Validate(), FieldIssue{"intro.content", "too_long"})
	d.Intro = &FormIntro{Title: "Sobre", Style: IntroParagraph, Content: []string{strings.Repeat("c", 300)}}
	if err := d.Validate(); err != nil {
		t.Fatalf("form refused: %v", err)
	}
	d.Intro.Content = []string{strings.Repeat("c", 150), strings.Repeat("c", 150)}
	requireIssues(t, d.Validate(), FieldIssue{"intro.content", "too_long"})
}

func TestIntroCardLines(t *testing.T) {
	list := FormIntro{Title: "T", Style: IntroList, Content: []string{"Um", "Dois"}}
	if got := list.Lines(); strings.Join(got, "|") != "Um|Dois" {
		t.Fatalf("list lines %q", got)
	}
	paragraph := FormIntro{Title: "T", Style: IntroParagraph, Content: []string{"Um", "Dois"}}
	if got := paragraph.Lines(); len(got) != 1 || got[0] != "Um\nDois" {
		t.Fatalf("paragraph lines %q", got)
	}
}

func TestLeadFormNeedsPrivacyAndAContactQuestion(t *testing.T) {
	if err := validForm().Validate(); err != nil {
		t.Fatalf("form refused: %v", err)
	}
	d := validForm()
	d.PrivacyURL = ""
	d.Questions = []FormQuestion{{Type: QuestionFullName}}
	requireIssues(t, d.Validate(), FieldIssue{"privacyUrl", "required"}, FieldIssue{"questions", "needs_phone_or_email"})
}

func TestCustomQuestionsGetAStableKeyAndCleanOptions(t *testing.T) {
	q := validForm().Questions[2]
	if q.Key != "qual_o_melhor_horario_c" || len(q.Options) != 2 {
		t.Fatalf("question %+v", q)
	}
}

func TestRepeatedStandardQuestionsAreRefused(t *testing.T) {
	d := validForm()
	d.Questions = append(d.Questions, FormQuestion{Type: QuestionPhone})
	requireIssues(t, d.Validate(), FieldIssue{"questions[3].type", "repeated"})
}

func TestFormLeadContactReadsMetaStandardFields(t *testing.T) {
	l := FormLead{Answers: map[string]string{"first_name": "Ana", "last_name": "Lima", "phone_number": "+55 11 98888-7777", "EMAIL": " Ana@X.com "}}
	c := l.Contact()
	if c.Name != "Ana Lima" || c.Phone != "5511988887777" || c.Email != "ana@x.com" {
		t.Fatalf("contact %+v", c)
	}
}

func TestFormLeadContactReadsTheAreaAnswers(t *testing.T) {
	cases := []struct {
		answers map[string]string
		want    LeadContact
	}{
		{map[string]string{"city": " São Paulo ", "STATE": "SP", "zip_code": "01310-100"}, LeadContact{City: "São Paulo", State: "SP", Zip: "01310-100"}},
		{map[string]string{"zip": "01310100"}, LeadContact{Zip: "01310100"}},
		{map[string]string{"post_code": "01310100", "province": "São Paulo"}, LeadContact{State: "São Paulo", Zip: "01310100"}},
		{map[string]string{"postal_code": "01310100"}, LeadContact{Zip: "01310100"}},
	}
	for _, tc := range cases {
		c := FormLead{Answers: tc.answers}.Contact()
		if c.City != tc.want.City || c.State != tc.want.State || c.Zip != tc.want.Zip {
			t.Errorf("answers %v: contact %+v, want %+v", tc.answers, c, tc.want)
		}
	}
}
