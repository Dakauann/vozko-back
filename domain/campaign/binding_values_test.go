package campaign

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/lead"
)

func TestBindingPlanValuesKeepsEverySlotEvenWhenOneIsEmpty(t *testing.T) {
	plan, err := PlanBindings([]VariableBinding{{Source: BindFirstName}, {Source: CustomBinding("escola")}, {Source: BindLiteral, Value: "1º ano"}, {Source: BindCity}}, 4, bindingDefs())
	if err != nil {
		t.Fatal(err)
	}
	subject := BindingSubject{Lead: &lead.Lead{Name: "Maria Souza"}}
	if got := plan.Values(subject); !reflect.DeepEqual(got, []string{"Maria", "", "1º ano", ""}) {
		t.Fatalf("Values() = %q", got)
	}
	_, resolveErr := plan.Resolve(subject)
	missing, ok := MissingVariablesOf(resolveErr)
	if !ok || !reflect.DeepEqual(missing, []MissingVariable{{Slot: 2, Source: CustomBinding("escola")}, {Slot: 4, Source: BindCity}}) {
		t.Fatalf("Resolve() missing = %+v, %v", missing, resolveErr)
	}
	if !errors.Is(resolveErr, ErrMissingVariable) {
		t.Fatalf("Resolve() = %v", resolveErr)
	}
	if got := plan.SourceOf(2); got != CustomBinding("escola") {
		t.Fatalf("SourceOf(2) = %q", got)
	}
	if got := plan.SourceOf(9); got != "" {
		t.Fatalf("SourceOf(9) = %q", got)
	}
}

func TestBindableSourcesAreTheFieldsAVariableCanTake(t *testing.T) {
	want := []BindingSource{BindFirstName, BindName, BindNickname, BindDistrict, BindCity, BindOwnerName}
	if got := BindableSources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BindableSources() = %v", got)
	}
	for _, source := range want {
		if _, err := PlanBindings([]VariableBinding{{Source: source}}, 1, nil); err != nil {
			t.Fatalf("%s is listed but refused: %v", source, err)
		}
	}
}

func TestBindableCustomFieldsLeaveSensitiveAndOtherObjectsOut(t *testing.T) {
	defs := append(bindingDefs(), nil, &customfield.Definition{Key: "cnpj", ObjectType: customfield.ObjectType("company"), Type: customfield.TypeText})
	got := BindableCustomFields(defs)
	keys := []string{}
	for _, def := range got {
		keys = append(keys, def.Key)
	}
	if !reflect.DeepEqual(keys, []string{"escola", "interesses"}) {
		t.Fatalf("BindableCustomFields() = %v", keys)
	}
}

func TestValidateBindingSourceAcceptsOnlyWhatPlanBindingsAccepts(t *testing.T) {
	defs := bindingDefs()
	cases := []struct {
		source BindingSource
		want   error
	}{
		{BindFirstName, nil},
		{CustomBinding("escola"), nil},
		{CustomBinding("classificacao"), ErrBindingSensitive},
		{CustomBinding("sumiu"), ErrBindingFieldUnknown},
		{BindingSource("lead.cpf"), ErrBindingUnknown},
		{BindLiteral, ErrBindingUnknown},
	}
	for _, tc := range cases {
		if err := ValidateBindingSource(tc.source, defs); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Fatalf("ValidateBindingSource(%q) = %v, want %v", tc.source, err, tc.want)
		}
	}
}
