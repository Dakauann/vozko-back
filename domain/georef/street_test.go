package georef

import (
	"testing"

	"vozko/domain/geo"
)

func TestStreetNameOf(t *testing.T) {
	tests := []struct {
		name               string
		kind, title, label string
		display, key       string
		ok                 bool
	}{
		{"kind, title and name", "RUA", "DOUTOR", "JOAO DA SILVA", "Rua Doutor Joao da Silva", "doutor joao da silva", true},
		{"accents fold in the key only", "AVENIDA", "", "GETÚLIO VARGAS", "Avenida Getúlio Vargas", "getulio vargas", true},
		{"roman numerals stay upper case", "RUA", "", "XV DE NOVEMBRO", "Rua XV de Novembro", "xv de novembro", true},
		{"extra spaces collapse", "  RUA ", "", "  DAS   FLORES ", "Rua das Flores", "das flores", true},
		{"no name", "RUA", "", "", "", "", false},
		{"unnamed street", "RUA", "", "SEM DENOMINACAO", "", "", false},
		{"unnamed street with accents", "RUA", "", "Sem Denominação", "", "", false},
		{"unnamed street spelled sem nome", "RUA", "", "SEM NOME", "", "", false},
		{"overlong name", "RUA", "", string(make([]byte, 201)), "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			display, key, ok := StreetNameOf(tt.kind, tt.title, tt.label)
			if ok != tt.ok || display != tt.display || key != tt.key {
				t.Fatalf("StreetNameOf(%q, %q, %q) = (%q, %q, %v), expected (%q, %q, %v)", tt.kind, tt.title, tt.label, display, key, ok, tt.display, tt.key, tt.ok)
			}
		})
	}
}

func TestPlaceDisplay(t *testing.T) {
	tests := map[string]string{
		"CIDADE SATELITE":     "Cidade Satelite",
		"BOA VIAGEM":          "Boa Viagem",
		"JARDIM DOS ESTADOS":  "Jardim dos Estados",
		"Água Nova":           "Água Nova",
		"SETOR E SUL":         "Setor E Sul",
		"QUADRA 104 BLOCO II": "Quadra 104 Bloco II",
		"":                    "",
	}
	for raw, want := range tests {
		if got := PlaceDisplay(raw); got != want {
			t.Fatalf("PlaceDisplay(%q) = %q, expected %q", raw, got, want)
		}
	}
}

func streetRecord(city, zip, locality, kind, name string, lat, lng float64, level int) Record {
	r := record(city, zip, locality, lat, lng, level)
	r.StreetKind, r.StreetName = kind, name
	return r
}

func TestBuilderKeepsOneStreetRowPerCEPStreetAndBairro(t *testing.T) {
	b := NewBuilder(DefaultLimits(), 1)
	b.Add(streetRecord("1400100", "69318240", "CENTRO", "RUA", "DAS FLORES", 2.82, -60.78, 1))
	b.Add(streetRecord("1400100", "69318240", "CENTRO", "RUA", "das flores", 2.82, -60.78, 1))
	b.Add(streetRecord("1400100", "69318240", "CENTRO", "RUA", "DAS FLORES", 2.82, -60.78, 6))
	b.Add(streetRecord("1400100", "69318240", "SAO VICENTE", "RUA", "DAS FLORES", 2.82, -60.78, 1))
	b.Add(streetRecord("1400100", "69318241", "CENTRO", "AVENIDA", "BRASIL", 2.83, -60.79, 1))
	b.Add(streetRecord("1400100", "69318242", "CENTRO", "RUA", "SEM DENOMINACAO", 2.83, -60.79, 1))
	streets := b.Streets()
	if len(streets) != 3 {
		t.Fatalf("Streets() = %+v, expected 3 distinct rows", streets)
	}
	first := streets[0]
	if first.ZipCode != "69318240" || first.CityCode != "1400100" || first.Key != "das flores" || first.DistrictKey != "centro" {
		t.Fatalf("first street = %+v, expected the CEP 69318240 row of Centro", first)
	}
	if first.Name != "Rua das Flores" || first.District != "Centro" || first.AddressCount != 3 {
		t.Fatalf("first street = %+v, expected the display name, the bairro and every address counted", first)
	}
	if streets[1].DistrictKey != "sao vicente" || streets[1].AddressCount != 1 {
		t.Fatalf("second street = %+v, expected the other bairro of the same CEP", streets[1])
	}
	if streets[2].Name != "Avenida Brasil" {
		t.Fatalf("third street = %+v, expected Avenida Brasil", streets[2])
	}
}

func TestBuilderStoresBoundsForCitiesAndBairros(t *testing.T) {
	b := NewBuilder(DefaultLimits(), 1)
	for i := 0; i < 100; i++ {
		b.Add(record("1400100", "69318240", "CENTRO", 2.80+float64(i)*0.001, -60.80+float64(i)*0.001, 1))
	}
	b.Add(record("1400100", "69318240", "CENTRO", 5.0, -50.0, 1))
	city := b.CityPoints()[0]
	if city.Bounds.South < 2.80 || city.Bounds.North > 2.90 || city.Bounds.West < -60.80 || city.Bounds.East > -60.70 {
		t.Fatalf("city bounds = %+v, expected the far outlier left out", city.Bounds)
	}
	if city.Bounds.North-city.Bounds.South < 0.09 {
		t.Fatalf("city bounds = %+v, expected the spread of the city", city.Bounds)
	}
	district := b.DistrictPoints()[0]
	if district.Bounds != city.Bounds {
		t.Fatalf("bairro bounds = %+v, expected the same rows as the city %+v", district.Bounds, city.Bounds)
	}
}

func TestBoundsOfASinglePointKeepAMinimumSpan(t *testing.T) {
	var s sample
	s.keep(geo.Point{Lat: -8.05, Lng: -34.9})
	bounds := s.bounds()
	if bounds.North-bounds.South < minBoundsSpanDeg || bounds.East-bounds.West < minBoundsSpanDeg {
		t.Fatalf("bounds = %+v, expected at least %v degrees per side", bounds, minBoundsSpanDeg)
	}
	if err := bounds.Validate(); err != nil {
		t.Fatalf("bounds = %+v invalid: %v", bounds, err)
	}
	var empty sample
	if got := empty.bounds(); got != (geo.BBox{}) {
		t.Fatalf("empty bounds = %+v, expected none", got)
	}
}

func TestCitiesCarryTheirBoundsAndAddressCount(t *testing.T) {
	bounds := geo.BBox{South: 2.8, West: -60.8, North: 2.9, East: -60.7}
	points := []CityPoint{{CityCode: "1400100", Point: geo.Point{Lat: 2.82, Lng: -60.67}, SampleCount: 10, Bounds: bounds}}
	cities, _ := Cities(points, map[string]Municipality{"1400100": {CityCode: "1400100", Name: "Boa Vista"}})
	if len(cities) != 1 || cities[0].Bounds != bounds || cities[0].AddressCount != 10 {
		t.Fatalf("Cities() = %+v, expected the bounds and the address count", cities)
	}
}
