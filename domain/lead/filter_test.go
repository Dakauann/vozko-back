package lead

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
)

func leadFilter(field crmfilter.Field, op crmfilter.Operator, values ...string) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: field, Operator: op, Values: values}},
	}}}
}

func TestValidateFilter_AcceptsTheLeadVocabulary(t *testing.T) {
	cases := []crmfilter.Filter{
		leadFilter(crmfilter.FieldOwner, crmfilter.OpIn, "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f", "ai:1b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f", "workflow:2b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"),
		leadFilter(crmfilter.FieldOwner, crmfilter.OpIsEmpty),
		leadFilter(crmfilter.FieldSource, crmfilter.OpIn, "manual", "import", "channel"),
		leadFilter(crmfilter.FieldGeoStatus, crmfilter.OpIn, "pending", "located", "quota_exceeded"),
		leadFilter(crmfilter.FieldGeoPrecision, crmfilter.OpIn, "exact", "postal_code", "city"),
		leadFilter(crmfilter.FieldRelationKind, crmfilter.OpIn, "parent", "referred_by"),
		leadFilter(crmfilter.FieldBlocked, crmfilter.OpIsTrue),
	}
	for _, f := range cases {
		if err := ValidateFilter(f); err != nil {
			t.Fatalf("ValidateFilter(%+v) = %v, want nil", f, err)
		}
	}
}

func TestValidateFilter_RefusesValuesNoLeadCanHold(t *testing.T) {
	cases := []struct {
		name string
		f    crmfilter.Filter
		want error
	}{
		{"an owner that is not an actor id", leadFilter(crmfilter.FieldOwner, crmfilter.OpIn, "fulano"), crmfilter.ErrInvalidValue},
		{"the system as owner", leadFilter(crmfilter.FieldOwner, crmfilter.OpEquals, "system"), crmfilter.ErrInvalidValue},
		{"an ai owner without a uuid", leadFilter(crmfilter.FieldOwner, crmfilter.OpEquals, "ai:abc"), crmfilter.ErrInvalidValue},
		{"an unknown source", leadFilter(crmfilter.FieldSource, crmfilter.OpEquals, "whatsapp"), crmfilter.ErrInvalidValue},
		{"an unknown geo status", leadFilter(crmfilter.FieldGeoStatus, crmfilter.OpEquals, "lost"), crmfilter.ErrInvalidValue},
		{"an unknown precision", leadFilter(crmfilter.FieldGeoPrecision, crmfilter.OpEquals, "planet"), crmfilter.ErrInvalidValue},
		{"an unknown relation kind", leadFilter(crmfilter.FieldRelationKind, crmfilter.OpEquals, "neighbour"), crmfilter.ErrInvalidValue},
		{"a predicate the registry refuses", leadFilter("bogus", crmfilter.OpEquals, "x"), crmfilter.ErrUnknownField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateFilter(tc.f); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateFilter() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCheckAddressFilter_KeepsStreetLevelFieldsForAddressReaders(t *testing.T) {
	street := []crmfilter.Filter{
		leadFilter(crmfilter.FieldZip, crmfilter.OpIn, "01310-100"),
		leadFilter(crmfilter.FieldGeoPrecision, crmfilter.OpIn, "exact"),
		leadFilter(crmfilter.FieldGeoStatus, crmfilter.OpIn, "located"),
		leadFilter(crmfilter.FieldArea, crmfilter.OpIn, "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"),
		leadFilter(crmfilter.FieldAreaApproximate, crmfilter.OpIn, "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"),
		leadFilter(crmfilter.FieldGeoPlacement, crmfilter.OpIn, "approximate"),
	}
	open := []crmfilter.Filter{
		leadFilter(crmfilter.FieldDistrict, crmfilter.OpIn, "sp:sao paulo/centro"),
		leadFilter(crmfilter.FieldCity, crmfilter.OpIn, "sp:sao paulo"),
		leadFilter(crmfilter.FieldState, crmfilter.OpIn, "SP"),
		leadFilter(crmfilter.FieldHasAddress, crmfilter.OpIsTrue),
	}
	areaOnly := Viewer{ReadsLeads: true}
	full := Viewer{ReadsLeads: true, ReadsAddresses: true}
	for _, f := range street {
		if err := CheckAddressFilter(f, areaOnly); !errors.Is(err, ErrLeadFilterAddressForbidden) {
			t.Errorf("CheckAddressFilter(%s) without read_addresses = %v, want ErrLeadFilterAddressForbidden", f.Groups[0].Predicates[0].Field, err)
		}
		if err := CheckAddressFilter(f, full); err != nil {
			t.Errorf("CheckAddressFilter(%s) with read_addresses = %v, want nil", f.Groups[0].Predicates[0].Field, err)
		}
	}
	for _, f := range open {
		if err := CheckAddressFilter(f, areaOnly); err != nil {
			t.Errorf("CheckAddressFilter(%s) = %v, bairro, city and state stay open", f.Groups[0].Predicates[0].Field, err)
		}
	}
	if ErrorCode(ErrLeadFilterAddressForbidden) != "lead_filter_address_forbidden" {
		t.Fatalf("code = %q", ErrorCode(ErrLeadFilterAddressForbidden))
	}
}

func TestOwnerColumns_SplitsEachOwnerIntoItsStoredPair(t *testing.T) {
	human, agent := "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f", "1b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"
	ids, kinds := OwnerColumns([]string{human, " ai:" + agent + " ", "", "workflow:" + agent})
	if !reflect.DeepEqual(ids, []string{human, agent, agent}) || !reflect.DeepEqual(kinds, []string{"human", "ai", "workflow"}) {
		t.Fatalf("OwnerColumns() = %v, %v", ids, kinds)
	}
}
