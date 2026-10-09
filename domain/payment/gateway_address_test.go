package payment

import (
	"reflect"
	"testing"
)

func TestMissingAddressFieldsNamesEveryBlankBillingField(t *testing.T) {
	tests := []struct {
		name     string
		address  *GatewayAddress
		expected []string
		complete bool
	}{
		{"no address misses everything", nil, []string{"CEP", "logradouro", "número", "bairro", "cidade", "estado"}, false},
		{"a complete address misses nothing", &GatewayAddress{ZipCode: "01310100", StreetName: "Av. Paulista", StreetNumber: "1000", Neighborhood: "Bela Vista", City: "São Paulo", FederalUnit: "SP"}, []string{}, true},
		{"blank text is missing", &GatewayAddress{ZipCode: "01310100", StreetName: "  ", StreetNumber: "1000", Neighborhood: "Bela Vista", City: "São Paulo", FederalUnit: "SP"}, []string{"logradouro"}, false},
		{"lists what is missing in order", &GatewayAddress{StreetName: "Av. Paulista"}, []string{"CEP", "número", "bairro", "cidade", "estado"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MissingAddressFields(tt.address); !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("MissingAddressFields() = %v, expected %v", got, tt.expected)
			}
			if got := tt.address.Complete(); got != tt.complete {
				t.Fatalf("Complete() = %v, expected %v", got, tt.complete)
			}
		})
	}
}
