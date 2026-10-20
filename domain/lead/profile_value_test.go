package lead

import (
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/shared"
)

func TestAProfileFieldReadsBackTheValueTheUpdateWouldStore(t *testing.T) {
	born := shared.DateOf(time.Date(1990, 5, 17, 0, 0, 0, 0, time.UTC))
	l := &Lead{
		Name: "Ana", Email: "ana@example.com", BirthDate: &born,
		CustomFields: map[string]any{"curso": "Direito"},
		Addresses:    []Address{{Primary: true, Postal: address.Postal{ZipCode: "01001000", City: "São Paulo", State: "SP"}}},
	}
	cases := []struct {
		field string
		want  interface{}
		found bool
	}{
		{CustomFieldName("curso"), "Direito", true},
		{CustomFieldName("turno"), nil, false},
		{FieldBirthDate, born.String(), true},
		{FieldEmail, "ana@example.com", true},
		{FieldAddresses, address.Postal{ZipCode: "01001000", City: "São Paulo", State: "SP"}, true},
		{"phones", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			got, found := l.ProfileValue(tc.field)
			if found != tc.found || got != tc.want {
				t.Fatalf("ProfileValue(%q) = %v, %v; want %v, %v", tc.field, got, found, tc.want, tc.found)
			}
		})
	}
}

func TestAnEmptyLeadHasNoProfileValues(t *testing.T) {
	l := &Lead{}
	for _, field := range []string{FieldBirthDate, FieldEmail, FieldAddresses, CustomFieldName("curso")} {
		if got, found := l.ProfileValue(field); found {
			t.Errorf("ProfileValue(%q) = %v on an empty lead", field, got)
		}
	}
}
