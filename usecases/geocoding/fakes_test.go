package geocoding_usecase

import (
	"context"
	"errors"
	"sync"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
)

var (
	now      = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	cepPoint = geo.Point{Lat: -23.561, Lng: -46.656}
	district = geo.Point{Lat: -23.565, Lng: -46.660}
	city     = geo.Point{Lat: -23.55, Lng: -46.63}
	errDown  = errors.New("down")
)

type fakeReference struct {
	coverage    geo.Coverage
	coverageErr error
	lookupErr   error
	index       geo.ReferenceIndex
	lookups     int
	looked      []address.Postal
}

func (f *fakeReference) Coverage(context.Context) (geo.Coverage, error) {
	return f.coverage, f.coverageErr
}

func (f *fakeReference) Lookup(_ context.Context, postals []address.Postal) (geo.ReferenceIndex, error) {
	f.lookups++
	f.looked = append(f.looked, postals...)
	return f.index, f.lookupErr
}

func loadedReference() *fakeReference {
	return &fakeReference{coverage: everyState(), index: geo.ReferenceIndex{
		CEPs: map[string]geo.ReferencePoint{
			"01310100": {Point: cepPoint, SpreadM: 120, CityCode: "3550308"},
			"01310900": {Point: cepPoint, SpreadM: 2400, CityCode: "3550308"},
		},
		Districts: map[address.Place]geo.ReferencePoint{{CityCode: "3550308", DistrictKey: "bela vista"}: {Point: district}},
		Cities:    map[string]geo.ReferencePoint{"3550308": {Point: city}},
	}}
}

type fakeProvider struct {
	mu      sync.Mutex
	outcome geo.Outcome
	err     error
	calls   []geo.Query
}

func (f *fakeProvider) Geocode(_ context.Context, q geo.Query) (geo.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, q)
	return f.outcome, f.err
}

type fakeSettings struct {
	byWorkspace map[string]geocoding.Settings
	err         error
	reads       int
}

func (f *fakeSettings) Settings(_ context.Context, ws string) (geocoding.Settings, error) {
	return f.byWorkspace[ws], f.err
}

func (f *fakeSettings) SettingsOf(_ context.Context, ids []string) (map[string]geocoding.Settings, error) {
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]geocoding.Settings{}
	for _, id := range ids {
		if s, ok := f.byWorkspace[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

func (f *fakeSettings) ChangeSettings(context.Context, string, func(geocoding.Settings) (geocoding.Settings, error)) (geocoding.Settings, error) {
	return geocoding.Settings{}, errors.New("not used")
}

type fakeUsage struct {
	used  map[string]geocoding.Usage
	moved map[string]bool
	err   error
	takes int
}

func (f *fakeUsage) TakeSlot(_ context.Context, ws string, slot geocoding.Slot) (bool, geocoding.Usage, error) {
	f.takes++
	if f.err != nil {
		return false, geocoding.Usage{}, f.err
	}
	current := f.used[ws].In(slot)
	if refusal, _ := slot.Refusal(current); refusal != geocoding.ExhaustedNone || f.moved[ws] {
		return false, current, nil
	}
	current.Requests++
	current.DayRequests++
	f.used[ws] = current
	return true, current, nil
}

func (f *fakeUsage) Usage(_ context.Context, ws string) (geocoding.Usage, error) {
	return f.used[ws], f.err
}

type fakeMetrics struct {
	mu          sync.Mutex
	backlog     map[string]int64
	outcomes    map[string]int
	provider    map[string]int
	quotaHits   map[string]int
	stale       int
	pauses      map[string]int
	pausedUntil map[string]time.Time
	reused      int
}

func newMetrics() *fakeMetrics {
	return &fakeMetrics{backlog: map[string]int64{}, outcomes: map[string]int{}, provider: map[string]int{}, quotaHits: map[string]int{}, pauses: map[string]int{}, pausedUntil: map[string]time.Time{}}
}

func (m *fakeMetrics) SetGeocodingProviderPausedUntil(provider, reason string, until time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pausedUntil[provider+":"+reason] = until
}

func (m *fakeMetrics) IncGeocodingProviderPause(provider, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauses[provider+":"+reason]++
}

func (m *fakeMetrics) IncGeocodingAnswerReused() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reused++
}

func (m *fakeMetrics) SetGeocodingBacklog(status string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backlog[status] = n
}

func (m *fakeMetrics) AddGeocodingOutcomes(outcome string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes[outcome] += n
}

func (m *fakeMetrics) ObserveGeocodingProvider(provider, result string, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider[provider+":"+result]++
}

func (m *fakeMetrics) IncGeocodingQuotaHit(quota string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quotaHits[quota]++
}

func (m *fakeMetrics) AddGeocodingStale(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stale += n
}

func claimOf(id, ws string, p address.Postal) geocoding.Claim {
	a := lead.Address{ID: id, Label: lead.AddressHome, Primary: true, Postal: p.Normalize(), GeoStatus: lead.GeoPending}
	return geocoding.Claim{AddressID: id, WorkspaceID: ws, LeadID: "lead-" + id, Address: a, Fingerprint: a.Fingerprint(), Attempts: 1}
}

func paulista(street string) address.Postal {
	return address.Postal{ZipCode: "01310900", Street: street, Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP"}
}

func enabled() geocoding.Settings { return geocoding.Settings{Provider: geocoding.ProviderOpenCage} }

func providerFix(precision geo.Precision) geo.Fix {
	return geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: precision, Source: geo.SourceProvider, Provider: "opencage", FixedAt: now}
}

func everyState() geo.Coverage {
	c := geo.Coverage{States: map[string]bool{}}
	for _, s := range address.IBGEStates() {
		c.States[s.State] = true
	}
	return c
}

type fakeNotifier struct {
	mu      sync.Mutex
	changes []lead.Change
}

func (f *fakeNotifier) LeadChanged(change lead.Change) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, change)
}

type fakeAnswers struct {
	mu         sync.Mutex
	stored     map[geocoding.AnswerKey]geocoding.Answer
	readErr    error
	writeErr   error
	reads      int
	asked      [][]geocoding.AnswerKey
	remembered []geocoding.Answer
}

func newAnswers() *fakeAnswers {
	return &fakeAnswers{stored: map[geocoding.AnswerKey]geocoding.Answer{}}
}

func (f *fakeAnswers) Answers(_ context.Context, keys []geocoding.AnswerKey) (map[geocoding.AnswerKey]geocoding.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	f.asked = append(f.asked, append([]geocoding.AnswerKey(nil), keys...))
	if f.readErr != nil {
		return nil, f.readErr
	}
	out := map[geocoding.AnswerKey]geocoding.Answer{}
	for _, k := range keys {
		if a, ok := f.stored[k]; ok {
			out[k] = a
		}
	}
	return out, nil
}

func (f *fakeAnswers) Remember(_ context.Context, answer geocoding.Answer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remembered = append(f.remembered, answer)
	if f.writeErr != nil {
		return f.writeErr
	}
	f.stored[answer.Key] = answer
	return nil
}

type fakePauses struct {
	mu      sync.Mutex
	pause   *geocoding.ProviderPause
	readErr error
	openErr error
	opened  []geocoding.ProviderPause
	reads   int
}

func (f *fakePauses) ProviderPause(_ context.Context, provider geocoding.Provider) (geocoding.ProviderPause, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.readErr != nil {
		return geocoding.ProviderPause{}, false, f.readErr
	}
	if f.pause == nil || f.pause.Provider != provider {
		return geocoding.ProviderPause{}, false, nil
	}
	return *f.pause, true, nil
}

func (f *fakePauses) OpenProviderPause(_ context.Context, pause geocoding.ProviderPause) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, pause)
	if f.openErr != nil {
		return f.openErr
	}
	f.pause = &pause
	return nil
}
