package campaignaudience

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

func filterSelection() *selection.Selection {
	filter := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldName, Operator: crmfilter.OpContains, Values: []string{"Maria"}}}}}}
	s := selection.ForFilter(filter)
	return &s
}

func TestSpecValidateRefusesAnAmbiguousOrIncompleteSource(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want error
	}{
		{name: "unknown source", spec: Spec{Source: "planilha"}, want: ErrSourceInvalid},
		{name: "leads without a selection", spec: Spec{Source: SourceLeads}, want: ErrSelectionRequired},
		{name: "leads with a list", spec: Spec{Source: SourceLeads, Selection: filterSelection(), ListID: "list-1"}, want: ErrSourceAmbiguous},
		{name: "list without an id", spec: Spec{Source: SourceList}, want: ErrListRequired},
		{name: "list with a selection", spec: Spec{Source: SourceList, ListID: "list-1", Selection: filterSelection()}, want: ErrSourceAmbiguous},
		{name: "invalid selection", spec: Spec{Source: SourceLeads, Selection: &selection.Selection{Mode: selection.ModeIDs}}, want: selection.ErrEmptySelection},
		{name: "duplicate key", spec: Spec{Source: SourceList, ListID: "l", Variables: []Variable{{Key: "1"}, {Key: " 1 "}}}, want: ErrVariableDuplicate},
		{name: "empty key", spec: Spec{Source: SourceList, ListID: "l", Variables: []Variable{{Key: " "}}}, want: ErrVariableUnknown},
		{name: "unknown policy", spec: Spec{Source: SourceList, ListID: "l", Variables: []Variable{{Key: "1", Missing: "skip"}}}, want: ErrMissingPolicyInvalid},
		{name: "text without text", spec: Spec{Source: SourceList, ListID: "l", Variables: []Variable{{Key: "1", Missing: MissingText, Text: "  "}}}, want: ErrTextRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.spec.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
	ok := Spec{Source: SourceLeads, Selection: filterSelection(), Variables: []Variable{{Key: "1", Source: campaign.BindFirstName, Missing: MissingText, Text: "cliente"}, {Key: "2", Missing: MissingExclude}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a complete spec is refused: %v", err)
	}
}

func TestSpecValidateForCreateNeedsTheCountTheSelectionWasShown(t *testing.T) {
	unconfirmed := Spec{Source: SourceLeads, Selection: filterSelection()}
	if err := unconfirmed.ValidateForCreate(); !errors.Is(err, selection.ErrCountRequired) {
		t.Fatalf("unconfirmed = %v", err)
	}
	confirmed := filterSelection()
	confirmed.ExpectedCount = 12
	if err := (Spec{Source: SourceLeads, Selection: confirmed}).ValidateForCreate(); err != nil {
		t.Fatalf("confirmed = %v", err)
	}
	if err := (Spec{Source: SourceList, ListID: "list-1"}).ValidateForCreate(); err != nil {
		t.Fatalf("a list needs no count: %v", err)
	}
}

func TestBindFillsEachSlotFromTheRequestOrItsSuggestion(t *testing.T) {
	slots := []Slot{{Key: "1", Suggested: campaign.BindFirstName}, {Key: "2", Suggested: campaign.BindDistrict}, {Key: "3"}}
	bound, err := Bind(slots, []Variable{{Key: "2", Source: campaign.BindCity, Missing: MissingExclude}, {Key: "1", Missing: MissingText, Text: "cliente"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Bound{
		{Variable: Variable{Key: "1", Source: campaign.BindFirstName, Missing: MissingText, Text: "cliente"}, Suggested: true},
		{Variable: Variable{Key: "2", Source: campaign.BindCity, Missing: MissingExclude}},
		{Variable: Variable{Key: "3"}},
	}
	if !reflect.DeepEqual(bound, want) {
		t.Fatalf("Bind() = %+v\nwant %+v", bound, want)
	}
	if _, err := Bind(slots, []Variable{{Key: "4"}}); !errors.Is(err, ErrVariableUnknown) {
		t.Fatalf("unknown key = %v", err)
	}
}

func TestBindKeepsTheFallbackOfAnEmailTag(t *testing.T) {
	bound, err := Bind([]Slot{{Key: "first_name", Fallback: "cliente"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bound[0].Fallback != "cliente" || bound[0].Source != "" {
		t.Fatalf("bound = %+v", bound[0])
	}
}

func TestBindingsOfBoundSlotsAreTheTemplateBindings(t *testing.T) {
	bound := []Bound{
		{Variable: Variable{Key: "1", Source: campaign.BindFirstName}},
		{Variable: Variable{Key: "2", Source: campaign.BindLiteral, Value: "1º ano"}},
		{Variable: Variable{Key: "3"}},
	}
	got := Bindings(bound)
	want := []campaign.VariableBinding{{Source: campaign.BindFirstName}, {Source: campaign.BindLiteral, Value: "1º ano"}, {Source: campaign.BindLiteral, Value: unboundPlaceholder}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Bindings() = %+v", got)
	}
	if !Unbound(bound[2]) || Unbound(bound[0]) {
		t.Fatal("only the slot without a source is unbound")
	}
}
