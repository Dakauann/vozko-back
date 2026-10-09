package geo

import (
	"reflect"
	"testing"
	"time"

	"vozko/domain/address"
)

func TestOutcomeConstructors(t *testing.T) {
	fix := Fix{Point: Point{Lat: -23.5, Lng: -46.6}, Precision: PrecisionStreet, Source: SourceReference}
	tests := []struct {
		name    string
		outcome Outcome
		kind    OutcomeKind
	}{
		{"located carries the fix", Located(fix), OutcomeLocated},
		{"not found", NotFound(), OutcomeNotFound},
		{"ambiguous", Ambiguous(), OutcomeAmbiguous},
		{"unavailable", Unavailable(ReasonProviderDown, time.Minute), OutcomeUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.outcome.Kind != tt.kind {
				t.Fatalf("kind = %q, expected %q", tt.outcome.Kind, tt.kind)
			}
		})
	}
	if got := Located(fix); got.Fix != fix {
		t.Fatalf("Located() lost the fix: %+v", got)
	}
	if got := Unavailable(ReasonRateLimited, -time.Second); got.RetryAfter != 0 || got.Reason != ReasonRateLimited {
		t.Fatalf("Unavailable() = %+v, expected a reason and no negative wait", got)
	}
}

func TestOutcomeLocatedWithAnInvalidFixIsUnavailable(t *testing.T) {
	got := Located(Fix{Precision: PrecisionExact, Source: SourceProvider})
	if got.Kind != OutcomeUnavailable || got.Reason != ReasonInvalidAnswer {
		t.Fatalf("Located(invalid) = %+v, expected unavailable with %q so nothing guessed is stored", got, ReasonInvalidAnswer)
	}
}

func TestReferenceKeysFor(t *testing.T) {
	postals := []address.Postal{
		{ZipCode: "01310-100", District: "Jd. Paulista", City: "São Paulo", State: "SP", CityCode: "3550308"},
		{ZipCode: "01310100", City: "Sao Paulo", State: "sp"},
		{District: "Centro", City: "Contagem", State: "MG"},
		{ZipCode: "bad", City: "", State: ""},
	}
	got := ReferenceKeysFor(postals)
	want := ReferenceKeys{
		ZipCodes:  []string{"01310100"},
		CityNames: []CityName{{State: "SP", NameKey: "sao paulo"}, {State: "MG", NameKey: "contagem"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReferenceKeysFor() = %+v, expected %+v", got, want)
	}
	if !ReferenceKeysFor(nil).Empty() {
		t.Fatalf("keys of nothing must be empty")
	}
}

func TestReferencePlacesFor(t *testing.T) {
	idx := ReferenceIndex{
		CEPs:      map[string]ReferencePoint{"01310100": {CityCode: "3550308"}},
		CityCodes: map[CityName]string{{State: "MG", NameKey: "contagem"}: "3118601"},
	}
	postals := []address.Postal{
		{ZipCode: "01310100", District: "Jd. Paulista", City: "Sao Paulo", State: "SP"},
		{District: "Centro", City: "Contagem", State: "MG"},
		{District: "Centro", City: "Contagem", State: "MG", CityCode: "3118601"},
		{City: "Belo Horizonte", State: "MG", CityCode: "3106200"},
		{District: "Centro", City: "Lugar Nenhum", State: "AC"},
	}
	got := idx.PlacesFor(postals)
	want := ReferencePlaces{
		Districts: []address.Place{{CityCode: "3550308", DistrictKey: "jardim paulista"}, {CityCode: "3118601", DistrictKey: "centro"}},
		CityCodes: []string{"3550308", "3118601", "3106200"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PlacesFor() = %+v, expected %+v", got, want)
	}
	if !(ReferenceIndex{}).PlacesFor(nil).Empty() {
		t.Fatalf("places of nothing must be empty")
	}
}

func TestReferenceIndexMerge(t *testing.T) {
	a := ReferenceIndex{CEPs: map[string]ReferencePoint{"01310100": {Count: 1}}, CityCodes: map[CityName]string{{State: "SP", NameKey: "sao paulo"}: "3550308"}}
	b := ReferenceIndex{Cities: map[string]ReferencePoint{"3550308": {Count: 2}}, Districts: map[address.Place]ReferencePoint{{CityCode: "3550308", DistrictKey: "se"}: {Count: 3}}}
	got := a.Merge(b)
	if len(got.CEPs) != 1 || len(got.Cities) != 1 || len(got.Districts) != 1 || len(got.CityCodes) != 1 {
		t.Fatalf("Merge() = %+v, expected every map kept", got)
	}
}

func TestReferenceIndexCandidates(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cep := Point{Lat: -23.561, Lng: -46.656}
	district := Point{Lat: -23.565, Lng: -46.660}
	city := Point{Lat: -23.55, Lng: -46.63}
	generic := Point{Lat: -18.6, Lng: -44.0}
	idx := ReferenceIndex{
		CEPs: map[string]ReferencePoint{
			"01310100": {Point: cep, SpreadM: 120, Count: 340, CityCode: "3550308"},
			"30140071": {Point: cep, SpreadM: 900, Count: 40, CityCode: "3106200"},
			"39270000": {Point: generic, SpreadM: 80, Count: 9000, CityCode: "3151800"},
		},
		Districts: map[address.Place]ReferencePoint{
			{CityCode: "3550308", DistrictKey: "jardim paulista"}: {Point: district, SpreadM: 800, Count: 4000},
			{CityCode: "3118601", DistrictKey: "centro"}:          {Point: district, SpreadM: 900, Count: 3000},
		},
		Cities: map[string]ReferencePoint{
			"3550308": {Point: city},
			"3118601": {Point: city},
			"3151800": {Point: generic},
		},
		CityCodes: map[CityName]string{{State: "MG", NameKey: "contagem"}: "3118601"},
	}
	ref := func(p Point, precision Precision) Fix {
		return Fix{Point: p, Precision: precision, Source: SourceReference, FixedAt: at}
	}
	tests := []struct {
		name   string
		postal address.Postal
		want   []Fix
	}{
		{
			"a tight CEP, its bairro and its city",
			address.Postal{ZipCode: "01310-100", District: "Jd Paulista", City: "São Paulo", State: "SP"},
			[]Fix{ref(cep, PrecisionStreet), ref(district, PrecisionDistrict), ref(city, PrecisionCity)},
		},
		{
			"a wide CEP is a postal code point",
			address.Postal{ZipCode: "30140071", City: "Belo Horizonte", State: "MG"},
			[]Fix{ref(cep, PrecisionPostalCode)},
		},
		{
			"a generic CEP is only a city, so the city code still finds the city point",
			address.Postal{ZipCode: "39270-000", City: "Pompéu", State: "MG"},
			[]Fix{ref(generic, PrecisionCity), ref(generic, PrecisionCity)},
		},
		{
			"without a CEP the city is found by its name and the bairro by the pair",
			address.Postal{District: "Centro", City: "Contagem", State: "MG"},
			[]Fix{ref(district, PrecisionDistrict), ref(city, PrecisionCity)},
		},
		{
			"a bairro of another city never matches",
			address.Postal{District: "Jardim Paulista", City: "Contagem", State: "MG"},
			[]Fix{ref(city, PrecisionCity)},
		},
		{
			"an unknown place has no candidates",
			address.Postal{ZipCode: "99999999", City: "Lugar Nenhum", State: "AC"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.Candidates(tt.postal, at)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Candidates() = %+v, expected %+v", got, tt.want)
			}
		})
	}
}

func TestCoverageCovers(t *testing.T) {
	partial := Coverage{States: map[string]bool{"SP": true, "RR": true}}
	all := Coverage{States: map[string]bool{}}
	for _, s := range address.IBGEStates() {
		all.States[s.State] = true
	}
	tests := []struct {
		name     string
		coverage Coverage
		postal   address.Postal
		want     bool
	}{
		{"a loaded state is covered", partial, address.Postal{City: "São Paulo", State: "sp"}, true},
		{"a state that was not loaded is not", partial, address.Postal{City: "Contagem", State: "MG"}, false},
		{"the city code names the state", partial, address.Postal{ZipCode: "69301000", CityCode: "1400100"}, true},
		{"a CEP alone needs every state loaded", partial, address.Postal{ZipCode: "01310100"}, false},
		{"a CEP alone is covered by a full load", all, address.Postal{ZipCode: "01310100"}, true},
		{"nothing loaded covers nothing", Coverage{}, address.Postal{City: "São Paulo", State: "SP"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.coverage.Covers(tt.postal); got != tt.want {
				t.Fatalf("Covers() = %v, want %v", got, tt.want)
			}
		})
	}
	if !all.Full() || partial.Full() {
		t.Fatal("Full() must be true only when every state is loaded")
	}
}

func TestOutcomeOfAnswers(t *testing.T) {
	house := func(lat, lng float64, precision Precision) Fix {
		return Fix{Point: Point{Lat: lat, Lng: lng}, Precision: precision, Source: SourceProvider, Provider: "opencage"}
	}
	paulista := house(-23.5613, -46.6565, PrecisionAddress)
	nearby := house(-23.5650, -46.6600, PrecisionAddress)
	campinas := house(-22.9056, -47.0608, PrecisionAddress)
	streetFar := house(-22.9056, -47.0608, PrecisionStreet)
	cityA := house(-23.5505, -46.6333, PrecisionCity)
	cityB := house(-22.9056, -47.0608, PrecisionCity)
	tests := []struct {
		name  string
		fixes []Fix
		want  Outcome
	}{
		{"no answer", nil, NotFound()},
		{"one answer", []Fix{paulista}, Located(paulista)},
		{"two houses close together", []Fix{paulista, nearby}, Located(paulista)},
		{"two houses far apart", []Fix{paulista, campinas}, Ambiguous()},
		{"a far answer of another precision", []Fix{paulista, streetFar}, Located(paulista)},
		{"two far cities do not pin a house", []Fix{cityA, cityB}, Located(cityA)},
		{"an invalid first answer", []Fix{{Precision: PrecisionAddress, Source: SourceProvider}}, Unavailable(ReasonInvalidAnswer, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OutcomeOfAnswers(tt.fixes); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("OutcomeOfAnswers() = %+v, expected %+v", got, tt.want)
			}
		})
	}
}
