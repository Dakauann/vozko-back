package campaign

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
)

func bindingDefs() []*customfield.Definition {
	return []*customfield.Definition{
		{Key: "escola", ObjectType: customfield.ObjectLead, Type: customfield.TypeText},
		{Key: "interesses", ObjectType: customfield.ObjectLead, Type: customfield.TypeMultiSelect},
		{Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Sensitive: true},
	}
}

func bindingLead() *lead.Lead {
	return &lead.Lead{
		ID: "lead-1", Number: "5511999990001", Name: "Maria Souza Lima", Nickname: "Mari",
		CustomFields: map[string]any{"escola": "Instituto Prisma", "interesses": []any{"matemática", "música"}, "classificacao": "Positivo"},
		Addresses: []lead.Address{
			{Postal: address.Postal{District: "Vila Nova", City: "Campinas"}},
			{Primary: true, Postal: address.Postal{District: "Jardim Silveira", City: "Barueri"}},
		},
	}
}

func TestPlanBindingsRefusesWhatCannotBeSent(t *testing.T) {
	cases := []struct {
		name     string
		bindings []VariableBinding
		slots    int
		want     error
	}{
		{name: "one binding per slot", bindings: []VariableBinding{{Source: BindFirstName}}, slots: 2, want: ErrBindingsMismatch},
		{name: "more bindings than slots", bindings: []VariableBinding{{Source: BindFirstName}, {Source: BindCity}}, slots: 1, want: ErrBindingsMismatch},
		{name: "an unknown source", bindings: []VariableBinding{{Source: "lead.cpf"}}, slots: 1, want: ErrBindingUnknown},
		{name: "a literal without text", bindings: []VariableBinding{{Source: BindLiteral, Value: "  "}}, slots: 1, want: ErrBindingLiteralEmpty},
		{name: "a lead source carrying a value", bindings: []VariableBinding{{Source: BindName, Value: "cliente"}}, slots: 1, want: ErrBindingUnknown},
		{name: "a custom field that does not exist", bindings: []VariableBinding{{Source: CustomBinding("cpf")}}, slots: 1, want: ErrBindingFieldUnknown},
		{name: "a custom field without a key", bindings: []VariableBinding{{Source: CustomBinding(" ")}}, slots: 1, want: ErrBindingFieldUnknown},
		{name: "a sensitive custom field", bindings: []VariableBinding{{Source: CustomBinding("classificacao")}}, slots: 1, want: ErrBindingSensitive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := PlanBindings(tc.bindings, tc.slots, bindingDefs()); !errors.Is(err, tc.want) {
				t.Fatalf("PlanBindings error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPlanBindingsAcceptsEveryLeadSource(t *testing.T) {
	bindings := []VariableBinding{
		{Source: BindLiteral, Value: " Matrículas 2027 "}, {Source: BindFirstName}, {Source: BindName}, {Source: BindNickname},
		{Source: BindDistrict}, {Source: BindCity}, {Source: BindOwnerName}, {Source: CustomBinding("escola")},
	}
	plan, err := PlanBindings(bindings, len(bindings), bindingDefs())
	if err != nil {
		t.Fatalf("PlanBindings: %v", err)
	}
	if !plan.NeedsLead() || !plan.NeedsAddresses() || !plan.NeedsOwnerNames() {
		t.Fatalf("plan needs = lead %v addresses %v owners %v, want all", plan.NeedsLead(), plan.NeedsAddresses(), plan.NeedsOwnerNames())
	}
}

func TestResolveBindingsReadsTheLead(t *testing.T) {
	cases := []struct {
		name    string
		binding VariableBinding
		subject BindingSubject
		want    string
	}{
		{name: "a literal", binding: VariableBinding{Source: BindLiteral, Value: " Matrículas 2027 "}, want: "Matrículas 2027"},
		{name: "the first name", binding: VariableBinding{Source: BindFirstName}, want: "Maria"},
		{name: "the name", binding: VariableBinding{Source: BindName}, want: "Maria Souza Lima"},
		{name: "the nickname", binding: VariableBinding{Source: BindNickname}, want: "Mari"},
		{name: "the district of the primary address", binding: VariableBinding{Source: BindDistrict}, want: "Jardim Silveira"},
		{name: "the city of the primary address", binding: VariableBinding{Source: BindCity}, want: "Barueri"},
		{name: "the owner name", binding: VariableBinding{Source: BindOwnerName}, subject: BindingSubject{OwnerName: " Ana Paula "}, want: "Ana Paula"},
		{name: "a text field", binding: VariableBinding{Source: CustomBinding("escola")}, want: "Instituto Prisma"},
		{name: "a multiselect field", binding: VariableBinding{Source: CustomBinding("interesses")}, want: "matemática, música"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanBindings([]VariableBinding{tc.binding}, 1, bindingDefs())
			if err != nil {
				t.Fatalf("PlanBindings: %v", err)
			}
			subject := tc.subject
			subject.Lead = bindingLead()
			got, err := plan.Resolve(subject)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("Resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveBindingsSkipsALeadWhoseSlotIsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		binding VariableBinding
		lead    *lead.Lead
	}{
		{name: "a name that is the lead's own number", binding: VariableBinding{Source: BindFirstName}, lead: &lead.Lead{Number: "5511999990001", Name: "5511999990001"}},
		{name: "no name", binding: VariableBinding{Source: BindName}, lead: &lead.Lead{Number: "5511999990001"}},
		{name: "no nickname", binding: VariableBinding{Source: BindNickname}, lead: &lead.Lead{Name: "Maria"}},
		{name: "no primary address", binding: VariableBinding{Source: BindDistrict}, lead: &lead.Lead{Addresses: []lead.Address{{Postal: address.Postal{District: "Centro"}}}}},
		{name: "a primary address without a city", binding: VariableBinding{Source: BindCity}, lead: &lead.Lead{Addresses: []lead.Address{{Primary: true}}}},
		{name: "no owner", binding: VariableBinding{Source: BindOwnerName}, lead: &lead.Lead{Owner: "member:1"}},
		{name: "an empty custom field", binding: VariableBinding{Source: CustomBinding("escola")}, lead: &lead.Lead{CustomFields: map[string]any{"escola": " "}}},
		{name: "a missing custom field", binding: VariableBinding{Source: CustomBinding("escola")}, lead: &lead.Lead{}},
		{name: "no lead at all", binding: VariableBinding{Source: BindName}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanBindings([]VariableBinding{tc.binding}, 1, bindingDefs())
			if err != nil {
				t.Fatalf("PlanBindings: %v", err)
			}
			if _, err := plan.Resolve(BindingSubject{Lead: tc.lead}); !errors.Is(err, ErrMissingVariable) {
				t.Fatalf("Resolve error = %v, want ErrMissingVariable", err)
			}
		})
	}
}

func TestALiteralOnlyPlanNeedsNoLeadData(t *testing.T) {
	plan, err := PlanBindings([]VariableBinding{{Source: BindLiteral, Value: "Olá"}}, 1, nil)
	if err != nil {
		t.Fatalf("PlanBindings: %v", err)
	}
	if plan.NeedsLead() || plan.NeedsAddresses() || plan.NeedsOwnerNames() {
		t.Fatal("a literal-only plan must not ask for lead data")
	}
	got, err := plan.Resolve(BindingSubject{})
	if err != nil || !reflect.DeepEqual(got, []string{"Olá"}) {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

func TestAPlanWithoutSlotsResolvesToNoValues(t *testing.T) {
	plan, err := PlanBindings(nil, 0, nil)
	if err != nil {
		t.Fatalf("PlanBindings: %v", err)
	}
	got, err := plan.Resolve(BindingSubject{Lead: bindingLead()})
	if err != nil || len(got) != 0 {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}
