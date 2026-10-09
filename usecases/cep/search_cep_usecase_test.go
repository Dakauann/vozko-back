package cep_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/cep"
	"vozko/domain/georef"
)

type fakeCache struct {
	stored  map[string]cep.CEPInfo
	readErr error
	saveErr error
	saved   []cep.CEPInfo
	checked []string
	reads   int
	clock   func() time.Time
}

func (f *fakeCache) stamp() time.Time {
	if f.clock == nil {
		return time.Time{}
	}
	return f.clock()
}

func (f *fakeCache) GetByCode(code string) (*cep.CEPInfo, error) {
	f.reads++
	if f.readErr != nil {
		return nil, f.readErr
	}
	if info, ok := f.stored[code]; ok {
		return &info, nil
	}
	return nil, nil
}

func (f *fakeCache) Save(info *cep.CEPInfo) error {
	f.saved = append(f.saved, *info)
	if f.saveErr != nil {
		return f.saveErr
	}
	if f.stored == nil {
		f.stored = map[string]cep.CEPInfo{}
	}
	stored := *info
	stored.CheckedAt = f.stamp()
	f.stored[info.Cep] = stored
	return nil
}

func (f *fakeCache) MarkChecked(code string) error {
	f.checked = append(f.checked, code)
	if info, ok := f.stored[code]; ok {
		info.CheckedAt = f.stamp()
		f.stored[code] = info
	}
	return nil
}

type fakeLookup struct {
	info  *cep.CEPInfo
	err   error
	codes []string
}

func (f *fakeLookup) Lookup(_ context.Context, code string) (*cep.CEPInfo, error) {
	f.codes = append(f.codes, code)
	if f.err != nil {
		return nil, f.err
	}
	info := *f.info
	return &info, nil
}

var paulista = cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"}

func TestSearchReturnsACachedCEPWithoutCallingTheLookup(t *testing.T) {
	cache := &fakeCache{stored: map[string]cep.CEPInfo{"01310100": paulista}}
	lookup := &fakeLookup{info: &paulista}

	info, err := NewSearchCEPUseCase(cache, lookup).Execute(context.Background(), "01310-100")
	if err != nil || *info != paulista {
		t.Fatalf("Execute() = %+v, %v", info, err)
	}
	if len(lookup.codes) != 0 {
		t.Fatalf("the lookup was called for a cached CEP: %v", lookup.codes)
	}
}

func TestSearchLooksUpAndCachesAnUnknownCEP(t *testing.T) {
	cache := &fakeCache{}
	lookup := &fakeLookup{info: &paulista}

	info, err := NewSearchCEPUseCase(cache, lookup).Execute(context.Background(), "01.310-100")
	if err != nil || *info != paulista {
		t.Fatalf("Execute() = %+v, %v", info, err)
	}
	if len(lookup.codes) != 1 || lookup.codes[0] != "01310100" {
		t.Fatalf("expected one lookup of the parsed CEP, got %v", lookup.codes)
	}
	if len(cache.saved) != 1 || cache.saved[0] != paulista {
		t.Fatalf("expected the result cached, got %v", cache.saved)
	}
}

func TestSearchRefreshesACachedCEPThatHasNoCityCode(t *testing.T) {
	legacy := paulista
	legacy.IBGE = ""
	cache := &fakeCache{stored: map[string]cep.CEPInfo{"01310100": legacy}}
	lookup := &fakeLookup{info: &paulista}

	info, err := NewSearchCEPUseCase(cache, lookup).Execute(context.Background(), "01310100")
	if err != nil || info.IBGE != "3550308" {
		t.Fatalf("expected the refreshed city code, got %+v, %v", info, err)
	}
	if len(cache.saved) != 1 || cache.saved[0].IBGE != "3550308" {
		t.Fatalf("expected the refreshed row cached, got %v", cache.saved)
	}
}

func TestSearchKeepsACachedCEPWhenTheRefreshFails(t *testing.T) {
	legacy := paulista
	legacy.IBGE = ""
	for name, lookup := range map[string]cep.Lookup{
		"lookup unavailable":    &fakeLookup{err: cep.ErrUnavailable},
		"lookup not configured": nil,
	} {
		t.Run(name, func(t *testing.T) {
			cache := &fakeCache{stored: map[string]cep.CEPInfo{"01310100": legacy}}
			info, err := NewSearchCEPUseCase(cache, lookup).Execute(context.Background(), "01310100")
			if err != nil || *info != legacy {
				t.Fatalf("expected the cached address, got %+v, %v", info, err)
			}
			if len(cache.saved) != 0 {
				t.Fatalf("nothing new to cache, got %v", cache.saved)
			}
		})
	}
}

func TestSearchStillAnswersWhenTheCacheWriteFails(t *testing.T) {
	cache := &fakeCache{saveErr: errors.New("db down")}
	info, err := NewSearchCEPUseCase(cache, &fakeLookup{info: &paulista}).Execute(context.Background(), "01310100")
	if err != nil || *info != paulista {
		t.Fatalf("Execute() = %+v, %v", info, err)
	}
}

func TestSearchRefusals(t *testing.T) {
	readErr := errors.New("db down")
	tests := []struct {
		name     string
		cache    cep.CEPRepository
		lookup   cep.Lookup
		raw      string
		expected error
	}{
		{"an invalid CEP", &fakeCache{}, &fakeLookup{info: &paulista}, "0131-010", cep.ErrInvalidCEP},
		{"an unknown CEP", &fakeCache{}, &fakeLookup{err: cep.ErrNotFound}, "01310100", cep.ErrNotFound},
		{"the lookup is down", &fakeCache{}, &fakeLookup{err: cep.ErrUnavailable}, "01310100", cep.ErrUnavailable},
		{"no lookup configured", &fakeCache{}, nil, "01310100", cep.ErrLookupNotConfigured},
		{"no cache configured", nil, &fakeLookup{info: &paulista}, "01310100", cep.ErrCacheNotConfigured},
		{"the cache cannot be read", &fakeCache{readErr: readErr}, &fakeLookup{info: &paulista}, "01310100", readErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewSearchCEPUseCase(tt.cache, tt.lookup).Execute(context.Background(), tt.raw)
			if !errors.Is(err, tt.expected) || info != nil {
				t.Fatalf("expected %v and no info, got %+v, %v", tt.expected, info, err)
			}
			if cache, ok := tt.cache.(*fakeCache); ok && len(cache.saved) != 0 {
				t.Fatalf("a refusal must cache nothing, got %v", cache.saved)
			}
		})
	}
}

func TestSearchDuringAnOutageAsksTheLookupOncePerInterval(t *testing.T) {
	legacy := paulista
	legacy.IBGE = ""
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cache := &fakeCache{stored: map[string]cep.CEPInfo{"01310100": legacy}}
	lookup := &fakeLookup{err: cep.ErrUnavailable}
	uc := NewSearchCEPUseCase(cache, lookup).(*searchCEPUseCase)
	uc.now = func() time.Time { return now }
	cache.clock = uc.now

	for range 3 {
		if info, err := uc.Execute(context.Background(), "01310100"); err != nil || info.Logradouro != legacy.Logradouro {
			t.Fatalf("expected the cached address, got %+v, %v", info, err)
		}
	}
	if len(lookup.codes) != 1 {
		t.Fatalf("an outage must cost one upstream call per interval, got %d", len(lookup.codes))
	}
	if len(cache.checked) != 1 || cache.checked[0] != "01310100" {
		t.Fatalf("the failed attempt must be recorded, got %v", cache.checked)
	}

	now = now.Add(cep.CityCodeRetryInterval)
	if _, err := uc.Execute(context.Background(), "01310100"); err != nil || len(lookup.codes) != 2 {
		t.Fatalf("after the interval the city code is asked again, got %d calls, %v", len(lookup.codes), err)
	}
}

func TestSearchKeepsAFreshAnswerWithoutACityCodeUntilTheInterval(t *testing.T) {
	noCode := paulista
	noCode.IBGE = ""
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cache := &fakeCache{}
	lookup := &fakeLookup{info: &noCode}
	uc := NewSearchCEPUseCase(cache, lookup).(*searchCEPUseCase)
	uc.now = func() time.Time { return now }
	cache.clock = uc.now

	for range 2 {
		if _, err := uc.Execute(context.Background(), "01310100"); err != nil {
			t.Fatal(err)
		}
	}
	if len(lookup.codes) != 1 || len(cache.saved) != 1 {
		t.Fatalf("an answer without a city code is final for the interval, got %d lookups and %d saves", len(lookup.codes), len(cache.saved))
	}
}

func TestSearchBoundsTheRefreshOfACachedRow(t *testing.T) {
	legacy := paulista
	legacy.IBGE = ""
	cache := &fakeCache{stored: map[string]cep.CEPInfo{"01310100": legacy}}
	lookup := &deadlineLookup{}

	if _, err := NewSearchCEPUseCase(cache, lookup).Execute(context.Background(), "01310100"); err != nil {
		t.Fatal(err)
	}
	if lookup.deadline <= 0 || lookup.deadline > refreshTimeout {
		t.Fatalf("a refresh must run under its own short deadline, got %v", lookup.deadline)
	}
}

type deadlineLookup struct{ deadline time.Duration }

func (d *deadlineLookup) Lookup(ctx context.Context, _ string) (*cep.CEPInfo, error) {
	if at, ok := ctx.Deadline(); ok {
		d.deadline = time.Until(at)
	}
	return nil, cep.ErrUnavailable
}

type fakeReference struct {
	rows  []georef.CEPStreetRow
	err   error
	codes []string
}

func (f *fakeReference) CEPStreets(_ context.Context, zip string) ([]georef.CEPStreetRow, error) {
	f.codes = append(f.codes, zip)
	return f.rows, f.err
}

func TestSearchFallsBackToTheReferenceWhenTheLookupIsDown(t *testing.T) {
	cache := &fakeCache{}
	reference := &fakeReference{rows: []georef.CEPStreetRow{{Street: "Avenida Paulista", District: "Bela Vista", CityCode: "3550308", City: "São Paulo", State: "SP", AddressCount: 40}}}
	lookup := &fakeLookup{err: cep.ErrUnavailable}
	info, err := NewSearchCEPUseCaseWithReference(cache, lookup, reference).Execute(context.Background(), "01310-100")
	if err != nil {
		t.Fatalf("Execute() err = %v, want the reference answer", err)
	}
	want := cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"}
	if *info != want {
		t.Fatalf("Execute() = %+v, want %+v", *info, want)
	}
	if len(cache.saved) != 0 || len(reference.codes) != 1 || reference.codes[0] != "01310100" {
		t.Fatalf("saved = %v, reference asked %v, want the reference answer served without caching it", cache.saved, reference.codes)
	}
}

func TestSearchNeverInventsACEPTheReferenceDoesNotKnow(t *testing.T) {
	for name, reference := range map[string]*fakeReference{
		"unknown":     {},
		"unreadable":  {err: errors.New("db down")},
		"no city row": {rows: []georef.CEPStreetRow{{Street: "Rua", CityCode: "3550308"}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewSearchCEPUseCaseWithReference(&fakeCache{}, &fakeLookup{err: cep.ErrUnavailable}, reference).Execute(context.Background(), "01310100")
			if !errors.Is(err, cep.ErrUnavailable) {
				t.Fatalf("Execute() err = %v, want the lookup outage", err)
			}
		})
	}
}

func TestSearchAsksTheReferenceOnlyWhenTheLookupIsDown(t *testing.T) {
	reference := &fakeReference{rows: []georef.CEPStreetRow{{Street: "Rua", CityCode: "3550308", City: "São Paulo", State: "SP"}}}
	if _, err := NewSearchCEPUseCaseWithReference(&fakeCache{}, &fakeLookup{err: cep.ErrNotFound}, reference).Execute(context.Background(), "01310100"); !errors.Is(err, cep.ErrNotFound) {
		t.Fatalf("Execute() err = %v, want the lookup's not found", err)
	}
	if _, err := NewSearchCEPUseCaseWithReference(&fakeCache{}, &fakeLookup{info: &paulista}, reference).Execute(context.Background(), "01310100"); err != nil {
		t.Fatal(err)
	}
	if len(reference.codes) != 0 {
		t.Fatalf("reference asked %v, want it untouched while the lookup answers", reference.codes)
	}
}
