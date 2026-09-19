package balance

import "testing"

// Every valid service type is enumerated by AllServiceTypes, and vice versa:
// the balance UI and the export iterate that list, so a type that is valid
// but not listed is money that never shows on a statement.
func TestServiceTypes_ValidAndEnumeratedAgree(t *testing.T) {
	for _, s := range AllServiceTypes() {
		if !s.IsValid() {
			t.Errorf("%q is enumerated but not valid", s)
		}
	}
	for _, s := range []ServiceType{ServiceAI, ServiceAddon} {
		found := false
		for _, e := range AllServiceTypes() {
			if e == s {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is valid but missing from AllServiceTypes", s)
		}
	}
	if ServiceType("bogus").IsValid() {
		t.Error("bogus must be invalid")
	}
}
