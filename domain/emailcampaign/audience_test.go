package emailcampaign

import (
	"errors"
	"reflect"
	"testing"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/emaildesign"
	"vozko/domain/selection"
)

func TestADraftTakesASelectionOrAListButNotBoth(t *testing.T) {
	sel := &selection.Selection{Mode: selection.ModeEveryone}
	both := Draft{Name: "Matrículas", Selection: sel, ListID: "list-1"}
	if err := both.Validate(); !errors.Is(err, ErrAudienceInvalid) || !errors.Is(err, audience.ErrSourceAmbiguous) {
		t.Fatalf("both = %v", err)
	}
	policies := Draft{Name: "Matrículas", ListID: "list-1", MergePolicies: []audience.Variable{{Key: "first_name", Missing: audience.MissingText}}}
	if err := policies.Validate(); !errors.Is(err, audience.ErrTextRequired) {
		t.Fatalf("a text policy without text = %v", err)
	}
	ok := Draft{Name: "Matrículas", ListID: " list-1 ", MergePolicies: []audience.Variable{{Key: " field.serie ", Missing: audience.MissingExclude}}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	var c Campaign
	c.Apply(ok)
	if c.ListID != "list-1" || c.Selection != nil || !reflect.DeepEqual(c.MergePolicies, []audience.Variable{{Key: "field.serie", Missing: audience.MissingExclude}}) {
		t.Fatalf("campaign = %+v", c)
	}
}

func TestTheAudienceSpecOfACampaignIsItsSelectionOrItsList(t *testing.T) {
	sel := &selection.Selection{Mode: selection.ModeEveryone}
	leads := Campaign{Selection: sel, MergePolicies: []audience.Variable{{Key: "first_name", Missing: audience.MissingExclude}}}
	spec, ok := leads.AudienceSpec()
	if !ok || spec.Source != audience.SourceLeads || spec.Selection != sel || len(spec.Variables) != 1 {
		t.Fatalf("spec = %+v, %v", spec, ok)
	}
	list := Campaign{ListID: "list-1"}
	if spec, ok := list.AudienceSpec(); !ok || spec.Source != audience.SourceList || spec.ListID != "list-1" {
		t.Fatalf("spec = %+v, %v", spec, ok)
	}
	if _, ok := (Campaign{}).AudienceSpec(); ok {
		t.Fatal("a campaign without audience has a spec")
	}
}

func TestMergePoliciesOverrideFallbacksAndListExcludedTags(t *testing.T) {
	c := Campaign{MergePolicies: []audience.Variable{
		{Key: "first_name", Missing: audience.MissingText, Text: "amigo"},
		{Key: "field.serie", Missing: audience.MissingExclude},
		{Key: "company"},
	}}
	p := c.Personalize(emaildesign.Personalization{FirstName: "", Fields: map[string]string{}})
	if p.Fallbacks["first_name"] != "amigo" || len(p.Fallbacks) != 1 {
		t.Fatalf("fallbacks = %v", p.Fallbacks)
	}
	if got := c.MissingExcluded(p); !reflect.DeepEqual(got, []string{"field.serie"}) {
		t.Fatalf("MissingExcluded() = %v", got)
	}
	p.Fields["serie"] = "1º ano"
	if got := c.MissingExcluded(p); got != nil {
		t.Fatalf("with the value = %v", got)
	}
}

func TestMissingVariableIsAnEmailSkipThatKeepsTheAddressUnique(t *testing.T) {
	if !SkipMissingVariable.Valid() || !SkipMissingVariable.KeepsAddressUnique() || string(SkipMissingVariable) != "missing_variable" {
		t.Fatal("missing_variable")
	}
}
