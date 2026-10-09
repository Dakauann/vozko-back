package address

import (
	"errors"
	"testing"

	"vozko/domain/cep"
)

func paulistaCEP() cep.CEPInfo {
	return cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"}
}

func TestCompletedByCEP(t *testing.T) {
	cases := []struct {
		name  string
		given Postal
		info  cep.CEPInfo
		want  Postal
		err   error
	}{
		{
			name:  "a bare CEP takes the street, district, city, state and city code of the lookup",
			given: Postal{ZipCode: "01310-100", Number: "1000"},
			info:  paulistaCEP(),
			want:  Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP", CityCode: "3550308"},
		},
		{
			name:  "what the person said about the street and district is kept",
			given: Postal{ZipCode: "01310100", Street: "Av. Paulista", District: "Jardins"},
			info:  paulistaCEP(),
			want:  Postal{ZipCode: "01310100", Street: "Av. Paulista", District: "Jardins", City: "São Paulo", State: "SP", CityCode: "3550308"},
		},
		{
			name:  "a city written without accents or a state written in full still matches",
			given: Postal{ZipCode: "01310100", City: "sao paulo", State: "São Paulo"},
			info:  paulistaCEP(),
			want:  Postal{ZipCode: "01310100", Street: "Avenida Paulista", District: "Bela Vista", City: "São Paulo", State: "SP", CityCode: "3550308"},
		},
		{
			name:  "a generic city CEP gives the city only",
			given: Postal{ZipCode: "13870000", Street: "Rua Sete", Number: "12"},
			info:  cep.CEPInfo{Cep: "13870000", Localidade: "São João da Boa Vista", Uf: "SP", IBGE: "3549102"},
			want:  Postal{ZipCode: "13870000", Street: "Rua Sete", Number: "12", City: "São João da Boa Vista", State: "SP", CityCode: "3549102"},
		},
		{
			name:  "a city that is not the CEP's is refused",
			given: Postal{ZipCode: "01310100", City: "Campinas"},
			info:  paulistaCEP(),
			err:   ErrCEPMismatch,
		},
		{
			name:  "a state that is not the CEP's is refused",
			given: Postal{ZipCode: "01310100", State: "RJ"},
			info:  paulistaCEP(),
			err:   ErrCEPMismatch,
		},
		{
			name:  "a lookup answer for another CEP is refused",
			given: Postal{ZipCode: "01310200"},
			info:  paulistaCEP(),
			err:   ErrCEPMismatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.given.CompletedBy(tc.info)
			if tc.err != nil {
				if !errors.Is(err, tc.err) || !errors.Is(err, ErrInvalidAddress) {
					t.Fatalf("err = %v, want %v as an invalid address", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CompletedBy: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}
