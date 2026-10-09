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

type fakeMetrics struct {
	mu       sync.Mutex
	backlog  map[string]int64
	outcomes map[string]int
	stale    int
}

func newMetrics() *fakeMetrics {
	return &fakeMetrics{backlog: map[string]int64{}, outcomes: map[string]int{}}
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

type chainSetup struct {
	reference *fakeReference
	metrics   *fakeMetrics
	notifier  *fakeNotifier
}

func newSetup() *chainSetup {
	return &chainSetup{reference: loadedReference(), metrics: newMetrics(), notifier: &fakeNotifier{}}
}
