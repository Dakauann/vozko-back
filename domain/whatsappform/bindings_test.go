package whatsappform

import (
	"testing"

	"vozko/domain/customfield"
)

func leadDefs() []*customfield.Definition {
	return []*customfield.Definition{
		{Key: "plano", Label: "Plano", Type: customfield.TypeSelect, Options: []string{"Opção o_a", "Opção o_b", "Outro"}, ObjectType: customfield.ObjectLead},
		{Key: "interesses", Label: "Interesses", Type: customfield.TypeMultiSelect, Options: []string{"Opção o_a", "Opção o_b"}, ObjectType: customfield.ObjectLead},
		{Key: "obs", Label: "Observação", Type: customfield.TypeText, ObjectType: customfield.ObjectLead},
		{Key: "renda", Label: "Renda", Type: customfield.TypeNumber, ObjectType: customfield.ObjectLead},
		{Key: "visita", Label: "Visita", Type: customfield.TypeDate, ObjectType: customfield.ObjectLead},
		{Key: "novidades", Label: "Novidades", Type: customfield.TypeBoolean, ObjectType: customfield.ObjectLead},
		{Key: "cpf", Label: "CPF", Type: customfield.TypeText, ObjectType: customfield.ObjectLead, Sensitive: true, LegalBasis: "contrato"},
	}
}

func bound(kind FieldKind, b Binding) *Field {
	f := &Field{Key: "f_x", Kind: kind, Label: "Campo", Required: true, Binding: &b}
	if kind.HasOptions() {
		f.Options = []Option{{ID: "o_a", Title: "Opção o_a"}, {ID: "o_b", Title: "Opção o_b"}}
	}
	return f
}

func TestCompatibleBindingsPass(t *testing.T) {
	cases := []struct {
		kind    FieldKind
		binding Binding
	}{
		{FieldShortText, Binding{Target: BindName}},
		{FieldEmail, Binding{Target: BindEmail}},
		{FieldDate, Binding{Target: BindBirthDate}},
		{FieldShortText, Binding{Target: BindAddressZip}},
		{FieldDropdown, Binding{Target: BindAddressState}},
		{FieldShortText, Binding{Target: BindAddressCity}},
		{FieldOptIn, Binding{Target: BindWhatsAppConsent}},
		{FieldRadio, Binding{Target: BindCustomField, CustomFieldKey: "plano"}},
		{FieldCheckbox, Binding{Target: BindCustomField, CustomFieldKey: "interesses"}},
		{FieldLongText, Binding{Target: BindCustomField, CustomFieldKey: "obs"}},
		{FieldPhone, Binding{Target: BindCustomField, CustomFieldKey: "obs"}},
		{FieldMultiChips, Binding{Target: BindCustomField, CustomFieldKey: "obs"}},
		{FieldNumber, Binding{Target: BindCustomField, CustomFieldKey: "renda"}},
		{FieldDate, Binding{Target: BindCustomField, CustomFieldKey: "visita"}},
		{FieldOptIn, Binding{Target: BindCustomField, CustomFieldKey: "novidades"}},
	}
	for _, tc := range cases {
		doc := oneScreen(fieldBlock(bound(tc.kind, tc.binding)))
		if problems := CheckBindings(doc, leadDefs()); len(problems) > 0 {
			t.Errorf("%s into %s %s must pass, got %+v", tc.kind, tc.binding.Target, tc.binding.CustomFieldKey, problems)
		}
	}
}

func TestBindingRefusals(t *testing.T) {
	cases := []struct {
		name  string
		field *Field
		code  string
	}{
		{"phone never becomes the identity", bound(FieldPhone, Binding{Target: BindName}), ProblemBindingKind},
		{"a photo has no lead field", bound(FieldPhoto, Binding{Target: BindCustomField, CustomFieldKey: "obs"}), ProblemBindingKind},
		{"email into a number", bound(FieldEmail, Binding{Target: BindCustomField, CustomFieldKey: "renda"}), ProblemBindingKind},
		{"text into consent", bound(FieldShortText, Binding{Target: BindWhatsAppConsent}), ProblemBindingKind},
		{"unknown custom field", bound(FieldShortText, Binding{Target: BindCustomField, CustomFieldKey: "nope"}), ProblemBindingUnknown},
		{"custom field without key", bound(FieldShortText, Binding{Target: BindCustomField}), ProblemBindingUnknown},
		{"unknown target", bound(FieldShortText, Binding{Target: "phone"}), ProblemInvalid},
		{"sensitive custom field", bound(FieldShortText, Binding{Target: BindCustomField, CustomFieldKey: "cpf"}), ProblemBindingSensitive},
		{"options outside the select", func() *Field {
			f := bound(FieldRadio, Binding{Target: BindCustomField, CustomFieldKey: "plano"})
			f.Options = append(f.Options, Option{ID: "o_c", Title: "Premium"})
			return f
		}(), ProblemBindingOptions},
		{"single choice into a multiselect", bound(FieldRadio, Binding{Target: BindCustomField, CustomFieldKey: "interesses"}), ProblemBindingKind},
		{"a sensitive answer is never copied to the lead", func() *Field {
			f := bound(FieldShortText, Binding{Target: BindCustomField, CustomFieldKey: "obs"})
			f.Sensitive = true
			return f
		}(), ProblemBindingSensitive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := CheckBindings(oneScreen(fieldBlock(tc.field)), leadDefs())
			if !hasProblem(problems, "b_f_x", "binding", tc.code) {
				t.Fatalf("want binding %s, got %+v", tc.code, problems)
			}
		})
	}
}

func TestTwoFieldsCannotWriteTheSameLeadField(t *testing.T) {
	first := bound(FieldEmail, Binding{Target: BindEmail})
	second := bound(FieldEmail, Binding{Target: BindEmail})
	second.Key = "f_y"
	doc := oneScreen(fieldBlock(first), Block{Key: "b_f_y", Field: second})
	if !hasProblem(CheckBindings(doc, leadDefs()), "b_f_y", "binding", ProblemBindingDuplicate) {
		t.Fatal("the second binding to the same target must be refused")
	}
}

func TestValidateRunsStructureAndBindings(t *testing.T) {
	doc := oneScreen(fieldBlock(bound(FieldPhone, Binding{Target: BindName})))
	doc.Categories = nil
	problems := Validate(doc, leadDefs())
	if !hasProblem(problems, "", "categories", ProblemRequired) || !hasProblem(problems, "b_f_x", "binding", ProblemBindingKind) {
		t.Fatalf("Validate must report both kinds of problems, got %+v", problems)
	}
}

func TestBindableKindsAreReadableForTheBuilder(t *testing.T) {
	targets := BindableKinds()
	if len(targets[BindEmail]) != 1 || targets[BindEmail][0] != FieldEmail {
		t.Fatalf("email binds only an email field, got %v", targets[BindEmail])
	}
	if _, ok := targets[BindCustomField]; ok {
		t.Fatal("custom fields depend on their type, so they are listed per field")
	}
	targets[BindEmail][0] = FieldPhone
	if BindableKinds()[BindEmail][0] != FieldEmail {
		t.Fatal("the caller gets a copy")
	}
	if kinds := CustomFieldKinds(customfield.TypeBoolean); len(kinds) != 1 || kinds[0] != FieldOptIn {
		t.Fatalf("a boolean custom field takes an opt-in, got %v", kinds)
	}
}
