package leadimport

import (
	"testing"

	"vozko/domain/lead"
)

func TestGrantsConsentWhenTheConsentColumnsAreMapped(t *testing.T) {
	cases := []struct {
		name    string
		columns []Column
		want    bool
	}{
		{"consent date", []Column{{Index: 0, Field: FieldNumber}, {Index: 1, Field: FieldConsentDate}}, true},
		{"consent date and purpose", []Column{{Index: 1, Field: FieldConsentDate}, {Index: 2, Field: FieldConsentPurpose}}, true},
		{"purpose alone", []Column{{Index: 2, Field: FieldConsentPurpose}}, true},
		{"spaced field name", []Column{{Index: 1, Field: " " + FieldConsentDate + " "}}, true},
		{"no consent column", []Column{{Index: 0, Field: FieldNumber}, {Index: 1, Field: FieldName}}, false},
		{"consent column left unmapped", []Column{{Index: 1, Header: "consentimento", Field: ""}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Settings{Columns: tc.columns, Policy: lead.PolicyFillEmpty}
			if got := s.GrantsConsent(); got != tc.want {
				t.Fatalf("GrantsConsent = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEveryCatalogFieldOfTheConsentGroupGrantsConsent(t *testing.T) {
	granting := 0
	for _, f := range Catalog(nil) {
		if GrantsConsentField(f) != (f.Group == GroupConsent) {
			t.Fatalf("field %s of group %s: GrantsConsentField = %v", f.Key, f.Group, GrantsConsentField(f))
		}
		if GrantsConsentField(f) {
			granting++
		}
	}
	if granting != 2 {
		t.Fatalf("%d catalog fields grant consent, want the date and the purpose", granting)
	}
}
