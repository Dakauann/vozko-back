package georef

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"vozko/domain/cep"
	"vozko/domain/geo"
)

func TestParsePlaceQuery(t *testing.T) {
	tests := []struct {
		name                 string
		kind, text, state    string
		cityCode             string
		wantErr              error
		kinds                []PlaceKind
		key, streetKey, zip  string
		wantState, wantScope string
	}{
		{name: "city prefix folds accents and case", kind: "city", text: "  SÃO Pau", kinds: []PlaceKind{PlaceCity}, key: "sao pau", streetKey: "sao pau"},
		{name: "city scoped to a state name", kind: "city", text: "rec", state: "pernambuco", kinds: []PlaceKind{PlaceCity}, key: "rec", streetKey: "rec", wantState: "PE"},
		{name: "city with an unknown state", kind: "city", text: "rec", state: "XX", wantErr: ErrPlaceScopeInvalid},
		{name: "district needs a city", kind: "district", text: "boa", wantErr: ErrPlaceScopeInvalid},
		{name: "district within a city", kind: "district", text: "Jd. Flo", cityCode: "2611606", kinds: []PlaceKind{PlaceDistrict}, key: "jardim flo", streetKey: "jardim flo", wantState: "PE", wantScope: "2611606"},
		{name: "street drops the leading kind word", kind: "street", text: "Av. Boa Vi", cityCode: "2611606", kinds: []PlaceKind{PlaceStreet}, key: "av boa vi", streetKey: "boa vi", wantState: "PE", wantScope: "2611606"},
		{name: "street that is only a kind word keeps it", kind: "street", text: "rua", cityCode: "2611606", kinds: []PlaceKind{PlaceStreet}, key: "rua", streetKey: "rua", wantState: "PE", wantScope: "2611606"},
		{name: "street with a bad city code", kind: "street", text: "boa", cityCode: "26116", wantErr: ErrPlaceScopeInvalid},
		{name: "one character is too short", kind: "city", text: " r ", wantErr: ErrPlaceQueryInvalid},
		{name: "only punctuation is empty", kind: "city", text: "--", wantErr: ErrPlaceQueryInvalid},
		{name: "sixty one characters are too long", kind: "city", text: strings.Repeat("a", 61), wantErr: ErrPlaceQueryInvalid},
		{name: "unknown kind", kind: "planet", text: "rec", wantErr: ErrPlaceQueryInvalid},
		{name: "cep kind with five digits", kind: "cep", text: "50030-", kinds: []PlaceKind{PlaceCEP}, zip: "50030"},
		{name: "cep kind with four digits", kind: "cep", text: "5003", wantErr: ErrPlaceQueryInvalid},
		{name: "cep kind with nine digits", kind: "cep", text: "500302300", wantErr: ErrPlaceQueryInvalid},
		{name: "cep kind with letters", kind: "cep", text: "5003a", wantErr: ErrPlaceQueryInvalid},
		{name: "mixed with digits searches CEPs", text: "50030-230", kinds: []PlaceKind{PlaceCEP}, zip: "50030230"},
		{name: "mixed with a name searches every named kind", text: "boa", kinds: []PlaceKind{PlaceCity, PlaceDistrict, PlaceStreet}, key: "boa", streetKey: "boa"},
		{name: "mixed scoped to a state", text: "boa", state: "pe", kinds: []PlaceKind{PlaceCity, PlaceDistrict, PlaceStreet}, key: "boa", streetKey: "boa", wantState: "PE"},
		{name: "mixed scoped to a city takes its state", text: "boa", cityCode: "2611606", kinds: []PlaceKind{PlaceCity, PlaceDistrict, PlaceStreet}, key: "boa", streetKey: "boa", wantState: "PE", wantScope: "2611606"},
		{name: "mixed with a city of another state", text: "boa", state: "SP", cityCode: "2611606", wantErr: ErrPlaceScopeInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ParsePlaceQuery(tt.kind, tt.text, tt.state, tt.cityCode)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParsePlaceQuery() error = %v, expected %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePlaceQuery() error = %v", err)
			}
			if !slices.Equal(q.Kinds, tt.kinds) || q.Key != tt.key || q.StreetKey != tt.streetKey || q.ZipPrefix != tt.zip {
				t.Fatalf("ParsePlaceQuery() = %+v, expected kinds %v key %q street key %q zip %q", q, tt.kinds, tt.key, tt.streetKey, tt.zip)
			}
			if q.State != tt.wantState || q.CityCode != tt.wantScope {
				t.Fatalf("ParsePlaceQuery() scope = (%q, %q), expected (%q, %q)", q.State, q.CityCode, tt.wantState, tt.wantScope)
			}
		})
	}
}

func TestPlaceQueryCacheKeyNamesKindScopePrefixAndGeneration(t *testing.T) {
	a, _ := ParsePlaceQuery("city", "São", "PE", "")
	b, _ := ParsePlaceQuery("city", "sao", "pe", "")
	c, _ := ParsePlaceQuery("city", "sao", "", "")
	d, _ := ParsePlaceQuery("", "sao", "PE", "")
	if a.CacheKey("g1") != b.CacheKey("g1") {
		t.Fatalf("one folded prefix must share a key: %q and %q", a.CacheKey("g1"), b.CacheKey("g1"))
	}
	keys := map[string]bool{a.CacheKey("g1"): true, a.CacheKey("g2"): true, c.CacheKey("g1"): true, d.CacheKey("g1"): true}
	if len(keys) != 4 {
		t.Fatalf("cache keys must differ by generation, scope and kind: %v", keys)
	}
}

func TestPlaceQueryCoverage(t *testing.T) {
	loaded := geo.Coverage{States: map[string]bool{"PE": true, "PB": true}}
	pe, _ := ParsePlaceQuery("city", "rec", "PE", "")
	sp, _ := ParsePlaceQuery("city", "sao", "SP", "")
	street, _ := ParsePlaceQuery("street", "boa", "", "3550308")
	mixed, _ := ParsePlaceQuery("", "boa", "", "")
	if err := pe.CoveredBy(loaded); err != nil {
		t.Fatalf("a loaded state refused: %v", err)
	}
	if err := sp.CoveredBy(loaded); !errors.Is(err, geo.ErrReferenceNotLoaded) {
		t.Fatalf("a state not loaded = %v, expected ErrReferenceNotLoaded", err)
	}
	if err := street.CoveredBy(loaded); !errors.Is(err, geo.ErrReferenceNotLoaded) {
		t.Fatalf("a city of a state not loaded = %v, expected ErrReferenceNotLoaded", err)
	}
	if err := mixed.CoveredBy(loaded); err != nil {
		t.Fatalf("an unscoped search over loaded states refused: %v", err)
	}
	if err := mixed.CoveredBy(geo.Coverage{}); !errors.Is(err, geo.ErrReferenceNotLoaded) {
		t.Fatalf("nothing loaded = %v, expected ErrReferenceNotLoaded", err)
	}
}

func TestCoveredStatesAreSorted(t *testing.T) {
	got := CoveredStates(geo.Coverage{States: map[string]bool{"RN": true, "DF": true, "PE": true, "XX": false}})
	if !slices.Equal(got, []string{"DF", "PE", "RN"}) {
		t.Fatalf("CoveredStates() = %v, expected [DF PE RN]", got)
	}
}

func TestRankPlacesPutsExactNamesFirstThenTheBusiestAndGroupsByKind(t *testing.T) {
	q, _ := ParsePlaceQuery("", "boa", "", "")
	places := []Place{
		{Kind: PlaceStreet, Key: "boa viagem", Name: "Avenida Boa Viagem", AddressCount: 900},
		{Kind: PlaceDistrict, Key: "boa viagem", Name: "Boa Viagem", AddressCount: 5000},
		{Kind: PlaceCity, Key: "boa vista", Name: "Boa Vista", AddressCount: 100},
		{Kind: PlaceDistrict, Key: "boa", Name: "Boa", AddressCount: 3},
		{Kind: PlaceStreet, Key: "boa esperanca", Name: "Rua Boa Esperanca", AddressCount: 50},
	}
	got := q.Rank(places)
	order := make([]string, len(got))
	for i, p := range got {
		order[i] = string(p.Kind) + ":" + p.Name
	}
	want := []string{"city:Boa Vista", "district:Boa", "district:Boa Viagem", "street:Avenida Boa Viagem", "street:Rua Boa Esperanca"}
	if !slices.Equal(order, want) {
		t.Fatalf("Rank() = %v, expected %v", order, want)
	}
}

func TestRankPlacesKeepsTheTopTenAcrossKinds(t *testing.T) {
	q, _ := ParsePlaceQuery("", "rua", "", "")
	var places []Place
	for i := 0; i < 12; i++ {
		places = append(places, Place{Kind: PlaceStreet, Key: "rua", Name: "Street", AddressCount: int64(100 + i)})
	}
	places = append(places, Place{Kind: PlaceCity, Key: "ruas", Name: "City", AddressCount: 1})
	got := q.Rank(places)
	if len(got) != PlaceLimit {
		t.Fatalf("Rank() kept %d, expected %d", len(got), PlaceLimit)
	}
	for _, p := range got {
		if p.Kind == PlaceCity {
			t.Fatalf("Rank() kept the least busy, inexact city over exact streets: %+v", got)
		}
	}
	if got[0].AddressCount != 111 {
		t.Fatalf("Rank() first = %+v, expected the busiest exact street", got[0])
	}
}

func TestPlaceLabels(t *testing.T) {
	tests := []struct {
		place Place
		want  string
	}{
		{Place{Kind: PlaceCity, Name: "Recife", City: "Recife", State: "PE"}, "Recife, PE"},
		{Place{Kind: PlaceDistrict, Name: "Boa Viagem", District: "Boa Viagem", City: "Recife", State: "PE"}, "Boa Viagem, Recife, PE"},
		{Place{Kind: PlaceStreet, Name: "Avenida Boa Viagem", Street: "Avenida Boa Viagem", District: "Boa Viagem", City: "Recife", State: "PE", ZipCode: "51020000"}, "Avenida Boa Viagem, Boa Viagem, Recife, PE"},
		{Place{Kind: PlaceStreet, Name: "Rua Sem Bairro", Street: "Rua Sem Bairro", City: "Recife", State: "PE"}, "Rua Sem Bairro, Recife, PE"},
		{Place{Kind: PlaceCEP, ZipCode: "51020000", Street: "Avenida Boa Viagem", District: "Boa Viagem", City: "Recife", State: "PE"}, "51020-000, Avenida Boa Viagem, Boa Viagem, Recife, PE"},
	}
	for _, tt := range tests {
		if got := tt.place.LabelText(); got != tt.want {
			t.Fatalf("LabelText(%+v) = %q, expected %q", tt.place, got, tt.want)
		}
	}
}

func TestReferenceGenerationChangesWithEveryLoad(t *testing.T) {
	at := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)
	one := ReferenceLoadsOf([]LoadStamp{{State: "PE", BuiltAt: at}, {State: "DF", BuiltAt: at}})
	same := ReferenceLoadsOf([]LoadStamp{{State: "DF", BuiltAt: at}, {State: "PE", BuiltAt: at}})
	reloaded := ReferenceLoadsOf([]LoadStamp{{State: "DF", BuiltAt: at}, {State: "PE", BuiltAt: at.Add(time.Minute)}})
	added := ReferenceLoadsOf([]LoadStamp{{State: "DF", BuiltAt: at}, {State: "PE", BuiltAt: at}, {State: "RN", BuiltAt: at}})
	if one.Generation == "" || one.Generation != same.Generation {
		t.Fatalf("the same loads in another order must share a generation: %q and %q", one.Generation, same.Generation)
	}
	if one.Generation == reloaded.Generation || one.Generation == added.Generation {
		t.Fatalf("a reload or a new state must change the generation")
	}
	if !one.Coverage.States["PE"] || !one.Coverage.States["DF"] || one.Coverage.States["RN"] {
		t.Fatalf("coverage = %+v, expected PE and DF", one.Coverage)
	}
	if empty := ReferenceLoadsOf(nil); empty.Generation == "" || len(empty.Coverage.States) != 0 {
		t.Fatalf("no loads = %+v, expected an empty coverage with a generation", empty)
	}
}

func TestCEPInfoFromStreets(t *testing.T) {
	recife := CEPStreetRow{CityCode: "2611606", City: "Recife", State: "PE"}
	row := func(street, district string, count int64) CEPStreetRow {
		r := recife
		r.Street, r.District, r.AddressCount = street, district, count
		return r
	}
	tests := []struct {
		name string
		rows []CEPStreetRow
		want *cep.CEPInfo
	}{
		{"one street and bairro", []CEPStreetRow{row("Avenida Boa Viagem", "Boa Viagem", 30)},
			&cep.CEPInfo{Cep: "51020000", Logradouro: "Avenida Boa Viagem", Bairro: "Boa Viagem", Localidade: "Recife", Uf: "PE", IBGE: "2611606"}},
		{"two streets leave the street empty", []CEPStreetRow{row("Rua A", "Boa Viagem", 3), row("Rua B", "Boa Viagem", 2)},
			&cep.CEPInfo{Cep: "51020000", Bairro: "Boa Viagem", Localidade: "Recife", Uf: "PE", IBGE: "2611606"}},
		{"two bairros leave the bairro empty", []CEPStreetRow{row("Rua A", "Boa Viagem", 3), row("Rua A", "Pina", 2)},
			&cep.CEPInfo{Cep: "51020000", Logradouro: "Rua A", Localidade: "Recife", Uf: "PE", IBGE: "2611606"}},
		{"nothing known", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CEPInfoFromStreets("51020000", tt.rows)
			if tt.want == nil {
				if ok {
					t.Fatalf("CEPInfoFromStreets() = %+v, expected nothing", got)
				}
				return
			}
			if !ok || got != *tt.want {
				t.Fatalf("CEPInfoFromStreets() = %+v, expected %+v", got, *tt.want)
			}
		})
	}
}

func TestCEPInfoFromStreetsTakesTheCityWithMostAddresses(t *testing.T) {
	rows := []CEPStreetRow{
		{Street: "Rua A", District: "Centro", CityCode: "2611606", City: "Recife", State: "PE", AddressCount: 2},
		{Street: "Rua A", District: "Centro", CityCode: "2607901", City: "Jaboatao", State: "PE", AddressCount: 9},
	}
	got, ok := CEPInfoFromStreets("51020000", rows)
	if !ok || got.IBGE != "2607901" || got.Localidade != "Jaboatao" || got.Logradouro != "" || got.Bairro != "" {
		t.Fatalf("CEPInfoFromStreets() = %+v, expected the busiest city and no street or bairro guessed across cities", got)
	}
}

func TestBestPointPrefersTheTightestReference(t *testing.T) {
	cepFix := geo.Fix{Point: geo.Point{Lat: -8.1, Lng: -34.9}, Precision: geo.CEPPrecision("51020230", 120), Source: geo.SourceReference}
	districtFix := geo.Fix{Point: geo.Point{Lat: -8.12, Lng: -34.91}, Precision: geo.PrecisionDistrict, Source: geo.SourceReference}
	point, precision, ok := BestPoint(districtFix, cepFix)
	if !ok || point != cepFix.Point || precision != geo.PrecisionStreet {
		t.Fatalf("BestPoint() = %+v %q %v, expected the CEP point at street precision", point, precision, ok)
	}
	if _, _, ok := BestPoint(); ok {
		t.Fatal("BestPoint() of nothing must report no point")
	}
}

func TestPlaceFilterKeysMatchTheLeadFilters(t *testing.T) {
	district := Place{Kind: PlaceDistrict, District: "Jd. Floresta", City: "São Paulo", State: "SP"}
	if got := district.CityKey(); got != "sp:sao paulo" {
		t.Fatalf("CityKey() = %q, expected the lead filter city key", got)
	}
	if got := district.DistrictPair(); got != "sp:sao paulo/jardim floresta" {
		t.Fatalf("DistrictPair() = %q, expected the lead filter bairro pair", got)
	}
	if got := (Place{Kind: PlaceCity, City: "Recife", State: "PE"}).DistrictPair(); got != "" {
		t.Fatalf("a city has no bairro pair, got %q", got)
	}
}
