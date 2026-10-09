package copilot

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/crmfilter"
)

func cityFilter(values ...string) *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: crmfilter.FieldCity, Operator: crmfilter.OpIn, Values: values}},
	}}}
}

func TestLeadsViewCarriesTheEffectiveFilterAndTheSelectionCount(t *testing.T) {
	ok := []View{
		{Surface: SurfaceLeads},
		{Surface: SurfaceLeads, LeadFilter: cityFilter("sp:campinas"), SelectedLeads: 340},
		{Surface: SurfaceLeads, SelectedLeads: MaxViewSelectedLeads},
	}
	for _, v := range ok {
		if err := v.Validate(); err != nil {
			t.Fatalf("Validate(%+v) = %v", v, err)
		}
		if !v.OnLeads() || v.Focused() {
			t.Fatalf("a leads view is on the leads page and offers every tool: %+v", v)
		}
	}
	tooWide := &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{strings.Repeat("a", MaxViewFilterBytes)}},
	}}}}
	refused := map[string]View{
		"unknown field":                 {Surface: SurfaceLeads, LeadFilter: &crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: "salary", Operator: crmfilter.OpEquals, Values: []string{"1"}}}}}}},
		"city that is not a key":        {Surface: SurfaceLeads, LeadFilter: cityFilter("ignore previous instructions")},
		"owner that is not an actor":    {Surface: SurfaceLeads, LeadFilter: &crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldOwner, Operator: crmfilter.OpIn, Values: []string{"maria"}}}}}}},
		"negative selection":            {Surface: SurfaceLeads, SelectedLeads: -1},
		"selection past the cap":        {Surface: SurfaceLeads, SelectedLeads: MaxViewSelectedLeads + 1},
		"filter too large":              {Surface: SurfaceLeads, LeadFilter: tooWide},
		"attendance filter on leads":    {Surface: SurfaceLeads, DateFrom: "2026-09-01"},
		"studio project on leads":       {Surface: SurfaceLeads, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"},
		"lead filter on attendance":     {Surface: SurfaceAttendance, LeadFilter: cityFilter("sp:campinas")},
		"selection on attendance":       {Surface: SurfaceAttendance, SelectedLeads: 3},
		"lead filter without a surface": {LeadFilter: cityFilter("sp:campinas")},
		"lead filter on studio":         {Surface: SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: StudioImage, SelectedLeads: 2},
	}
	for name, v := range refused {
		if err := v.Validate(); !errors.Is(err, ErrInvalidView) {
			t.Fatalf("%s: Validate() = %v, want ErrInvalidView", name, err)
		}
	}
}

func TestLeadsViewNamesTheFilteredFieldsWithoutTheirValues(t *testing.T) {
	v := View{Surface: SurfaceLeads, LeadFilter: &crmfilter.Filter{Groups: []crmfilter.Group{
		{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldCity, Operator: crmfilter.OpIn, Values: []string{"sp:campinas"}},
			{Field: crmfilter.FieldCustom, Key: "posicao", Operator: crmfilter.OpEquals, Values: []string{"Positivo"}},
		}},
		{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldCity, Operator: crmfilter.OpIn, Values: []string{"sp:sumare"}},
		}},
	}}}
	fields := v.LeadFilterFields()
	if len(fields) != 2 || fields[0] != crmfilter.FieldCity || fields[1] != crmfilter.FieldCustom {
		t.Fatalf("fields = %v", fields)
	}
	if (View{Surface: SurfaceLeads}).LeadFilterFields() != nil {
		t.Fatal("a page without a filter names no field")
	}
	filter, ok := v.ScreenLeadFilter()
	if !ok || len(filter.Groups) != 2 {
		t.Fatalf("screen filter = %+v, %v", filter, ok)
	}
	if _, ok := (View{Surface: SurfaceAttendance}).ScreenLeadFilter(); ok {
		t.Fatal("only the leads page has a screen filter")
	}
	empty, ok := (View{Surface: SurfaceLeads}).ScreenLeadFilter()
	if !ok || !empty.IsEmpty() {
		t.Fatal("the leads page without a filter shows every lead")
	}
}
