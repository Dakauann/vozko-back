package advertising

import "testing"

func validForm() LeadFormDraft {
	d := LeadFormDraft{
		AdAccountID: "a", PageID: "p", Name: "Orçamento", PrivacyURL: "https://loja.example.com/privacidade",
		Questions: []FormQuestion{{Type: QuestionFullName}, {Type: QuestionPhone}, {Type: QuestionCustom, Label: "Qual o melhor horário?", Options: []string{"Manhã", "Tarde", " "}}},
	}
	d.Normalize()
	return d
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
