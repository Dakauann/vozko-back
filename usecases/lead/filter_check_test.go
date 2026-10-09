package lead_usecase

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
)

func TestFilterChecks_ValidateALeadFilterForTheSaver(t *testing.T) {
	checks, err := NewFilterChecks(fakePermissions{"leads:read": true, "leads:read_addresses": true}, &fakeDefinitions{defs: leadDefinitions()})
	if err != nil {
		t.Fatal(err)
	}
	ok := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "interesse", Operator: crmfilter.OpEquals, Values: []string{"alto"}},
		{Field: crmfilter.FieldGeoStatus, Operator: crmfilter.OpIn, Values: []string{"pending"}},
	}}}}
	if err := checks.CheckLeadFilter(operator(), ok); err != nil {
		t.Fatalf("CheckLeadFilter(valid) = %v", err)
	}

	sensitive := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"positivo"}},
	}}}}
	if err := checks.CheckLeadFilter(operator(), sensitive); !errors.Is(err, customfield.ErrFilterSensitive) {
		t.Fatalf("a sensitive predicate for a saver without the permission = %v", err)
	}

	unknownStatus := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldGeoStatus, Operator: crmfilter.OpIn, Values: []string{"lost"}},
	}}}}
	if err := checks.CheckLeadFilter(operator(), unknownStatus); !errors.Is(err, lead.ErrLeadFilterInvalid) || !errors.Is(err, crmfilter.ErrInvalidValue) {
		t.Fatalf("an impossible value = %v, want ErrLeadFilterInvalid with its sentinel", err)
	}

	areaOnly, _ := NewFilterChecks(fakePermissions{"leads:read": true}, &fakeDefinitions{defs: leadDefinitions()})
	if err := areaOnly.CheckLeadFilter(operator(), ok); !errors.Is(err, lead.ErrLeadFilterAddressForbidden) {
		t.Fatalf("a map status predicate for a saver without leads:read_addresses = %v", err)
	}

	stranger, _ := NewFilterChecks(fakePermissions{}, &fakeDefinitions{defs: leadDefinitions()})
	if err := stranger.CheckLeadFilter(operator(), ok); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("a saver without leads:read = %v", err)
	}
	if _, err := NewFilterChecks(nil, &fakeDefinitions{}); err == nil {
		t.Fatal("filter checks without permissions must not be built")
	}
}

func TestFilterChecks_AnAreaMembershipKeyIsCheckedLikeAnyValue(t *testing.T) {
	checks, err := NewFilterChecks(fakePermissions{"leads:read": true, "leads:read_addresses": true}, &fakeDefinitions{defs: leadDefinitions()})
	if err != nil {
		t.Fatal(err)
	}
	area := func(key string) crmfilter.Filter {
		return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldArea, Key: key, Operator: crmfilter.OpIn, Values: []string{"4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11"}},
		}}}}
	}
	if err := checks.CheckLeadFilter(operator(), area(crmfilter.AreaExactOnly)); err != nil {
		t.Fatalf("an area with exact positions only = %v, want accepted", err)
	}
	if err := checks.CheckLeadFilter(operator(), area("with_approximate")); !errors.Is(err, crmfilter.ErrAreaMembershipInvalid) {
		t.Fatalf("the retired with_approximate key = %v, want ErrAreaMembershipInvalid", err)
	}
	if err := checks.CheckLeadFilter(operator(), area("everyone")); !errors.Is(err, lead.ErrLeadFilterInvalid) || !errors.Is(err, crmfilter.ErrAreaMembershipInvalid) {
		t.Fatalf("an unknown area membership = %v, want ErrLeadFilterInvalid", err)
	}
	if code := lead.ErrorCode(checks.CheckLeadFilter(operator(), area("everyone"))); code != "lead_filter_invalid" {
		t.Fatalf("code = %q, want lead_filter_invalid", code)
	}
}
