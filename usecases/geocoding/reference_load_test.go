package geocoding_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/geo"
	"vozko/domain/georef"
)

type fakeSource struct {
	records []georef.Record
	err     error
}

func (f fakeSource) Stream(_ context.Context, _ georef.UFFile, each func(georef.Record)) (int64, error) {
	for _, r := range f.records {
		each(r)
	}
	return int64(len(f.records)), f.err
}

type fakeStore struct {
	ceps           []georef.CEPPoint
	districts      []georef.DistrictPoint
	cities         []georef.City
	streets        []georef.Street
	streetsOf      []string
	loadsAtStreets []int
	loads          []georef.Load
	err            error
}

func (f *fakeStore) ReplaceStreets(_ context.Context, state string, s []georef.Street, _ time.Time) error {
	f.streets = append(f.streets, s...)
	f.streetsOf = append(f.streetsOf, state)
	f.loadsAtStreets = append(f.loadsAtStreets, len(f.loads))
	return f.err
}

func (f *fakeStore) UpsertCEPs(_ context.Context, p []georef.CEPPoint, _ time.Time) error {
	f.ceps = append(f.ceps, p...)
	return f.err
}

func (f *fakeStore) UpsertDistricts(_ context.Context, p []georef.DistrictPoint, _ time.Time) error {
	f.districts = append(f.districts, p...)
	return f.err
}

func (f *fakeStore) UpsertCities(_ context.Context, c []georef.City, _ time.Time) error {
	f.cities = append(f.cities, c...)
	return f.err
}

func (f *fakeStore) RecordLoad(_ context.Context, l georef.Load) error {
	f.loads = append(f.loads, l)
	return f.err
}

type fakeWaker struct {
	store       *fakeStore
	loadsAtWake []int
	at          []time.Time
	err         error
}

func (f *fakeWaker) WakeUnavailable(_ context.Context, at time.Time) (int64, error) {
	f.loadsAtWake = append(f.loadsAtWake, len(f.store.loads))
	f.at = append(f.at, at)
	return 3, f.err
}

func roraima() []georef.Record {
	return []georef.Record{
		{CityCode: "1400100", ZipCode: "69318240", Locality: "CIDADE SATELITE", StreetKind: "RUA", StreetName: "DAS FLORES", Point: geo.Point{Lat: 2.82, Lng: -60.78}, Level: 1},
		{CityCode: "1400100", ZipCode: "69318240", Locality: "CIDADE SATELITE", Point: geo.Point{Lat: 2.8201, Lng: -60.7801}, Level: 1},
		{CityCode: "1400159", ZipCode: "69380000", Locality: "CENTRO", Point: geo.Point{Lat: 3.36, Lng: -59.83}, Level: 1},
	}
}

func TestLoadWritesEveryTableAndRecordsTheLoadLast(t *testing.T) {
	store := &fakeStore{}
	waker := &fakeWaker{store: store}
	loader, err := NewReferenceLoader(ReferenceLoaderDeps{
		Source: fakeSource{records: roraima()}, Store: store, Waker: waker,
		Municipalities: map[string]georef.Municipality{"1400100": {CityCode: "1400100", Name: "Boa Vista", State: "RR"}},
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	load, err := loader.Load(context.Background(), georef.UFFile{Code: "14", State: "RR"}, false)
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if len(store.ceps) != 2 || len(store.districts) != 2 || len(store.cities) != 1 || len(store.loads) != 1 {
		t.Fatalf("store = %d ceps, %d districts, %d cities, %d loads", len(store.ceps), len(store.districts), len(store.cities), len(store.loads))
	}
	if len(store.streets) != 1 || store.streets[0].Name != "Rua das Flores" || len(store.streetsOf) != 1 || store.streetsOf[0] != "RR" || store.loadsAtStreets[0] != 0 || load.Streets != 1 {
		t.Fatalf("streets = %+v of %v, load = %+v, want the named street of RR written before the load is recorded", store.streets, store.streetsOf, load)
	}
	if load.State != "RR" || load.Rows != 3 || load.Measurement.CEPs != 2 || load.Measurement.Street != 1 || load.Source != georef.Attribution {
		t.Fatalf("Load() = %+v", load)
	}
	if len(load.MissingCities) != 1 || load.MissingCities[0] != "1400159" {
		t.Fatalf("missing cities = %v, want the city the directory does not name", load.MissingCities)
	}
	if len(waker.loadsAtWake) != 1 || waker.loadsAtWake[0] != 1 || !waker.at[0].Equal(now) || load.Woken != 3 {
		t.Fatalf("woken = %+v, load = %+v, want the waiting addresses woken once, after the load was recorded", waker, load)
	}
}

func TestAFailedWakeIsReportedAfterTheLoadIsRecorded(t *testing.T) {
	store := &fakeStore{}
	loader, _ := NewReferenceLoader(ReferenceLoaderDeps{Source: fakeSource{records: roraima()}, Store: store, Waker: &fakeWaker{store: store, err: errDown}, Municipalities: map[string]georef.Municipality{}})
	if _, err := loader.Load(context.Background(), georef.UFFile{Code: "14", State: "RR"}, false); !errors.Is(err, errDown) {
		t.Fatalf("Load() err = %v, want the wake error so the run is retried", err)
	}
	if len(store.loads) != 1 {
		t.Fatal("the load itself is recorded before waking the queue")
	}
}

func TestADryLoadWritesNothing(t *testing.T) {
	store := &fakeStore{}
	waker := &fakeWaker{store: store}
	loader, _ := NewReferenceLoader(ReferenceLoaderDeps{Source: fakeSource{records: roraima()}, Store: store, Waker: waker, Municipalities: map[string]georef.Municipality{}})
	if _, err := loader.Load(context.Background(), georef.UFFile{Code: "14", State: "RR"}, true); err != nil {
		t.Fatal(err)
	}
	if len(store.ceps)+len(store.loads)+len(waker.at) != 0 {
		t.Fatal("a dry load must not write or wake anything")
	}
}

func TestAFailedReadRecordsNoLoad(t *testing.T) {
	store := &fakeStore{}
	loader, _ := NewReferenceLoader(ReferenceLoaderDeps{Source: fakeSource{records: roraima(), err: errDown}, Store: store, Waker: &fakeWaker{store: store}, Municipalities: map[string]georef.Municipality{}})
	if _, err := loader.Load(context.Background(), georef.UFFile{Code: "14", State: "RR"}, false); !errors.Is(err, errDown) {
		t.Fatalf("Load() err = %v, want the read error", err)
	}
	if len(store.ceps)+len(store.loads) != 0 {
		t.Fatal("a half read file must not be written or marked as loaded")
	}
}

func TestAFileWithoutPointsRecordsNoLoad(t *testing.T) {
	store := &fakeStore{}
	loader, _ := NewReferenceLoader(ReferenceLoaderDeps{Source: fakeSource{}, Store: store, Waker: &fakeWaker{store: store}, Municipalities: map[string]georef.Municipality{}})
	if _, err := loader.Load(context.Background(), georef.UFFile{Code: "14", State: "RR"}, false); !errors.Is(err, ErrReferenceEmpty) {
		t.Fatalf("Load() err = %v, want ErrReferenceEmpty", err)
	}
	if len(store.loads) != 0 {
		t.Fatal("an empty file must never mark the state as covered")
	}
}

func TestNewReferenceLoaderRefusesMissingDependencies(t *testing.T) {
	store := &fakeStore{}
	waker := &fakeWaker{store: store}
	if _, err := NewReferenceLoader(ReferenceLoaderDeps{Store: store, Waker: waker}); err == nil {
		t.Fatal("a loader without a source must refuse")
	}
	if _, err := NewReferenceLoader(ReferenceLoaderDeps{Source: fakeSource{}, Waker: waker}); err == nil {
		t.Fatal("a loader without a store must refuse")
	}
	if _, err := NewReferenceLoader(ReferenceLoaderDeps{Source: fakeSource{}, Store: store}); err == nil {
		t.Fatal("a loader that cannot wake the waiting addresses must refuse")
	}
}
