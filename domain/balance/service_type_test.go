package balance

import "testing"

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
