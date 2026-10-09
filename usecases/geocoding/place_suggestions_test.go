package geocoding_usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/georef"
)

type fakePlaceIndex struct {
	stamps   []georef.LoadStamp
	loadsErr error
	readErr  error
	byKind   map[georef.PlaceKind][]georef.Place
	reads    []georef.PlaceKind
	queries  []georef.PlaceQuery
}

func (f *fakePlaceIndex) Loads(context.Context) ([]georef.LoadStamp, error) {
	return f.stamps, f.loadsErr
}

func (f *fakePlaceIndex) read(kind georef.PlaceKind, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	f.reads = append(f.reads, kind)
	f.queries = append(f.queries, q)
	if limit != georef.PlaceLimit {
		return nil, errors.New("unexpected limit")
	}
	return f.byKind[kind], f.readErr
}

func (f *fakePlaceIndex) Cities(_ context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	return f.read(georef.PlaceCity, q, limit)
}

func (f *fakePlaceIndex) Districts(_ context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	return f.read(georef.PlaceDistrict, q, limit)
}

func (f *fakePlaceIndex) Streets(_ context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	return f.read(georef.PlaceStreet, q, limit)
}

func (f *fakePlaceIndex) CEPs(_ context.Context, q georef.PlaceQuery, limit int) ([]georef.Place, error) {
	return f.read(georef.PlaceCEP, q, limit)
}

func placeSetup() (*fakePlaceIndex, *mapMemo, *busyGate, *PlaceSuggestions) {
	built := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)
	index := &fakePlaceIndex{
		stamps: []georef.LoadStamp{{State: "PE", BuiltAt: built}, {State: "DF", BuiltAt: built}},
		byKind: map[georef.PlaceKind][]georef.Place{
			georef.PlaceCity:     {{Kind: georef.PlaceCity, Key: "recife", Name: "Recife", City: "Recife", State: "PE", AddressCount: 700}},
			georef.PlaceDistrict: {{Kind: georef.PlaceDistrict, Key: "recreio", Name: "Recreio", District: "Recreio", City: "Recife", State: "PE", AddressCount: 50}},
			georef.PlaceStreet:   {{Kind: georef.PlaceStreet, Key: "rec", Name: "Rua Rec", Street: "Rua Rec", City: "Recife", State: "PE", AddressCount: 5, Precision: geo.PrecisionStreet}},
		},
	}
	memo := &mapMemo{values: map[string][]byte{}}
	gate := &busyGate{}
	service, err := NewPlaceSuggestions(PlaceSuggestionDeps{Index: index, Memo: memo, Gate: gate})
	if err != nil {
		panic(err)
	}
	return index, memo, gate, service
}

func TestNewPlaceSuggestionsRefusesMissingDependencies(t *testing.T) {
	index, memo, gate := &fakePlaceIndex{}, &mapMemo{values: map[string][]byte{}}, &busyGate{}
	for name, deps := range map[string]PlaceSuggestionDeps{
		"index": {Memo: memo, Gate: gate},
		"memo":  {Index: index, Gate: gate},
		"gate":  {Index: index, Memo: memo},
	} {
		if _, err := NewPlaceSuggestions(deps); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("NewPlaceSuggestions() without %s = %v, want a refusal naming it", name, err)
		}
	}
}

func TestSuggestRanksEveryNamedKindAndNamesTheCoveredStates(t *testing.T) {
	index, memo, gate, service := placeSetup()
	answer, err := service.Suggest(context.Background(), "", "rec", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(answer.CoveredStates, []string{"DF", "PE"}) {
		t.Fatalf("covered = %v, want DF and PE", answer.CoveredStates)
	}
	kinds := make([]georef.PlaceKind, len(answer.Places))
	for i, p := range answer.Places {
		kinds[i] = p.Kind
	}
	if !slices.Equal(kinds, []georef.PlaceKind{georef.PlaceCity, georef.PlaceDistrict, georef.PlaceStreet}) {
		t.Fatalf("places = %+v, want one of each named kind grouped", answer.Places)
	}
	if !slices.Equal(index.reads, []georef.PlaceKind{georef.PlaceCity, georef.PlaceDistrict, georef.PlaceStreet}) || gate.acquired != 1 {
		t.Fatalf("reads = %v under %d gate slots, want the three named kinds under one", index.reads, gate.acquired)
	}
	if answer.Places[2].Precision != geo.PrecisionStreet {
		t.Fatalf("a cached place lost its precision: %+v", answer.Places[2])
	}
	if len(memo.keys) != 1 || !strings.HasPrefix(memo.keys[0], "georef:places:") || !strings.HasSuffix(memo.keys[0], ":all:::rec") {
		t.Fatalf("memo keys = %v, want one key by generation, kind, scope and folded prefix", memo.keys)
	}
}

func TestSuggestServesARepeatFromTheMemo(t *testing.T) {
	index, _, _, service := placeSetup()
	for i := 0; i < 2; i++ {
		if _, err := service.Suggest(context.Background(), "city", "REC", "pe", ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(index.reads) != 1 {
		t.Fatalf("reads = %v, want the second search served from the memo", index.reads)
	}
}

func TestSuggestReadsOnlyTheScopedKind(t *testing.T) {
	index, _, _, service := placeSetup()
	if _, err := service.Suggest(context.Background(), "street", "av rec", "", "2611606"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(index.reads, []georef.PlaceKind{georef.PlaceStreet}) || index.queries[0].CityCode != "2611606" || index.queries[0].StreetKey != "rec" {
		t.Fatalf("reads = %v %+v, want streets of the city by their folded name", index.reads, index.queries)
	}
}

func TestSuggestRefusesAStateNotLoadedAndSaysWhichAre(t *testing.T) {
	index, memo, _, service := placeSetup()
	answer, err := service.Suggest(context.Background(), "city", "sao", "SP", "")
	if !errors.Is(err, geo.ErrReferenceNotLoaded) {
		t.Fatalf("Suggest() err = %v, want ErrReferenceNotLoaded", err)
	}
	if !slices.Equal(answer.CoveredStates, []string{"DF", "PE"}) || len(index.reads) != 0 || len(memo.keys) != 0 {
		t.Fatalf("answer = %+v, reads = %v, memo = %v, want the covered states and nothing read", answer, index.reads, memo.keys)
	}
}

func TestSuggestRefusesABadQueryBeforeAnyRead(t *testing.T) {
	index, _, _, service := placeSetup()
	if _, err := service.Suggest(context.Background(), "city", "r", "", ""); !errors.Is(err, georef.ErrPlaceQueryInvalid) {
		t.Fatalf("Suggest() err = %v, want ErrPlaceQueryInvalid", err)
	}
	if _, err := service.Suggest(context.Background(), "street", "rua", "", ""); !errors.Is(err, georef.ErrPlaceScopeInvalid) {
		t.Fatalf("Suggest() err = %v, want ErrPlaceScopeInvalid", err)
	}
	if len(index.reads) != 0 {
		t.Fatalf("reads = %v, want none", index.reads)
	}
}

func TestSuggestFailsClosedWhenTheReferenceCannotBeRead(t *testing.T) {
	index, _, _, service := placeSetup()
	index.loadsErr = errDown
	if _, err := service.Suggest(context.Background(), "city", "rec", "", ""); !errors.Is(err, geo.ErrReferenceUnavailable) {
		t.Fatalf("Suggest() err = %v, want ErrReferenceUnavailable", err)
	}
	index.loadsErr, index.readErr = nil, errDown
	if _, err := service.Suggest(context.Background(), "city", "rec", "", ""); !errors.Is(err, geo.ErrReferenceUnavailable) {
		t.Fatalf("Suggest() err = %v, want ErrReferenceUnavailable on a failed read", err)
	}
}

func TestSuggestPassesABusyGateThrough(t *testing.T) {
	_, _, gate, service := placeSetup()
	gate.busy = true
	if _, err := service.Suggest(context.Background(), "city", "rec", "", ""); !errors.Is(err, cache.ErrGateBusy) {
		t.Fatalf("Suggest() err = %v, want ErrGateBusy", err)
	}
}
