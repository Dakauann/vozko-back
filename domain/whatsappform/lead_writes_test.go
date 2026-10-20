package whatsappform

import (
	"reflect"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/lead"
)

func boundDocument() Document {
	bind := func(f *Field, b Binding) Block {
		f.Binding = &b
		return fieldBlock(f)
	}
	state := choiceField("f_state", FieldDropdown, "o_sp", "o_rj")
	state.Options[0].Title, state.Options[1].Title = "SP", "RJ"
	plan := choiceField("f_plan", FieldRadio, "o_a", "o_b")
	tags := choiceField("f_tags", FieldCheckbox, "o_a", "o_b")
	free := choiceField("f_free", FieldMultiChips, "o_a", "o_b")
	return Document{
		Schema: DocumentSchema, Categories: []Category{CategoryLeadGeneration},
		Screens: []Screen{{Key: "s_one", Title: "Dados", SubmitLabel: "Enviar", Blocks: []Block{
			bind(textField("f_name", "Nome"), Binding{Target: BindName}),
			bind(&Field{Key: "f_mail", Kind: FieldEmail, Label: "E-mail"}, Binding{Target: BindEmail}),
			bind(&Field{Key: "f_birth", Kind: FieldDate, Label: "Nascimento"}, Binding{Target: BindBirthDate}),
			bind(textField("f_zip", "CEP"), Binding{Target: BindAddressZip}),
			bind(textField("f_city", "Cidade"), Binding{Target: BindAddressCity}),
			bind(state, Binding{Target: BindAddressState}),
			bind(plan, Binding{Target: BindCustomField, CustomFieldKey: "plano"}),
			bind(tags, Binding{Target: BindCustomField, CustomFieldKey: "interesses"}),
			bind(free, Binding{Target: BindCustomField, CustomFieldKey: "obs"}),
			bind(&Field{Key: "f_income", Kind: FieldNumber, Label: "Renda"}, Binding{Target: BindCustomField, CustomFieldKey: "renda"}),
			bind(&Field{Key: "f_news", Kind: FieldOptIn, Label: "Novidades"}, Binding{Target: BindCustomField, CustomFieldKey: "novidades"}),
			bind(&Field{Key: "f_ok", Kind: FieldOptIn, Label: "Aceito receber mensagens"}, Binding{Target: BindWhatsAppConsent}),
			fieldBlock(textField("f_loose", "Solto")),
		}}},
	}
}

const boundResponse = `{
	"f_name": "Ana Souza", "f_mail": "ana@example.com", "f_birth": "1990-03-15",
	"f_zip": "01310-100", "f_city": "São Paulo", "f_state": "o_sp",
	"f_plan": "o_b", "f_tags": ["o_a", "o_b"], "f_free": ["o_a", "o_b"],
	"f_income": "4500", "f_news": false, "f_ok": true, "f_loose": "x"
}`

func TestAnswersBecomeALeadProfile(t *testing.T) {
	doc := boundDocument()
	answers, err := ParseResponse(doc, []byte(boundResponse))
	if err != nil {
		t.Fatal(err)
	}
	got := ToProfile(doc, answers, leadDefs())
	want := lead.Profile{
		Name:      "Ana Souza",
		Email:     "ana@example.com",
		BirthDate: "1990-03-15",
		Address:   address.Postal{ZipCode: "01310-100", City: "São Paulo", State: "SP"},
		CustomFields: map[string]string{
			"plano": "Opção o_b", "interesses": "Opção o_a|Opção o_b", "obs": "Opção o_a, Opção o_b",
			"renda": "4500", "novidades": "false",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profile = %+v\nwant      %+v", got, want)
	}
}

func TestUnboundAnswersLeaveTheProfileEmpty(t *testing.T) {
	doc := oneScreen(fieldBlock(textField("f_name", "Nome")))
	answers, _ := ParseResponse(doc, []byte(`{"f_name":"Ana"}`))
	if !ToProfile(doc, answers, leadDefs()).Empty() {
		t.Fatal("an answer without a binding writes nothing")
	}
}

func TestACheckedConsentOptInGrantsWhatsAppConsentWithTheFormAsEvidence(t *testing.T) {
	doc := boundDocument()
	answers, _ := ParseResponse(doc, []byte(boundResponse))
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	got := ToConsent(doc, answers, "resp-1", at)
	want := &lead.ConsentChange{
		Kind: lead.ConsentGrant, Source: lead.ConsentForm, Actor: lead.ConsentActor{Type: lead.ConsentActorLead},
		Purpose:  "Aceito receber mensagens",
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceFormSubmission, Ref: "resp-1", Excerpt: "Aceito receber mensagens"},
		At:       at,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("consent = %+v\nwant      %+v", got, want)
	}
	if err := lead.ConsentRules(*got); err != nil {
		t.Fatalf("the grant must satisfy the lead consent rules: %v", err)
	}
}

func TestAnUncheckedConsentNeverWithdraws(t *testing.T) {
	doc := boundDocument()
	answers, _ := ParseResponse(doc, []byte(`{"f_ok": false}`))
	if got := ToConsent(doc, answers, "resp-1", time.Now()); got != nil {
		t.Fatalf("an unchecked box is not a withdrawal, got %+v", got)
	}
}
