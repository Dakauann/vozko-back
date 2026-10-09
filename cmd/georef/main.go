package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/georef"
	"vozko/infra/cnefe"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	georef_repository "vozko/infra/repositories/georef"
	lead_repository "vozko/infra/repositories/lead"
	geocoding_usecase "vozko/usecases/geocoding"
)

func main() {
	dir := flag.String("dir", "", "folder holding the CNEFE zips ({code}_{UF}.zip) and municipios.json")
	ufs := flag.String("uf", "", "comma separated states to load (default: all 27)")
	download := flag.Bool("download", false, "download missing files from IBGE, resuming partial downloads")
	dry := flag.Bool("dry", false, "read and measure only, write nothing")
	flag.Parse()
	if strings.TrimSpace(*dir) == "" {
		log.Fatal("georef: -dir is required")
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatalf("georef: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	files := cnefe.Files{Dir: *dir, Download: *download, Downloader: cnefe.NewDownloader(&http.Client{Timeout: 2 * time.Hour})}
	targets, err := selectStates(*ufs)
	if err != nil {
		log.Fatalf("georef: %v", err)
	}
	municipalities, err := files.Municipalities(ctx)
	if err != nil {
		log.Fatalf("georef: municipalities: %v", err)
	}
	store, waker := georef.Store(dryStore{}), geocoding_usecase.AddressWaker(dryStore{})
	if !*dry {
		store, waker = openStore()
	}
	loader, err := geocoding_usecase.NewReferenceLoader(geocoding_usecase.ReferenceLoaderDeps{
		Source: files, Store: store, Waker: waker, Municipalities: municipalities,
	})
	if err != nil {
		log.Fatalf("georef: %v", err)
	}
	failed := 0
	for _, uf := range targets {
		started := time.Now()
		load, err := loader.Load(ctx, uf, *dry)
		if err != nil {
			failed++
			log.Printf("georef: %s failed: %v", uf.State, err)
			continue
		}
		m := load.Measurement
		log.Printf("georef: %s rows=%d pinned=%d count_only=%d skipped=%d ceps=%d street=%d (%.1f%%) postal_code=%d city=%d districts=%d cities=%d streets=%d missing_cities=%d woken=%d took=%s",
			uf.State, load.Rows, load.Stats.Pinned, load.Stats.CountOnly, load.Stats.Skipped, m.CEPs, m.Street, 100*m.StreetShare(),
			m.PostalCode, m.City, load.Districts, load.Cities, load.Streets, len(load.MissingCities), load.Woken, time.Since(started).Round(time.Second))
		runtime.GC()
	}
	if failed > 0 {
		log.Fatalf("georef: %d of %d states failed; rerun them with -uf", failed, len(targets))
	}
}

func selectStates(raw string) ([]georef.UFFile, error) {
	if strings.TrimSpace(raw) == "" {
		return georef.UFFiles(), nil
	}
	var out []georef.UFFile
	for _, part := range strings.Split(raw, ",") {
		uf, ok := georef.UFFileFor(part)
		if !ok {
			return nil, &unknownState{state: part}
		}
		out = append(out, uf)
	}
	return out, nil
}

type unknownState struct{ state string }

func (e *unknownState) Error() string { return "unknown state " + strings.TrimSpace(e.state) }

func openStore() (georef.Store, geocoding_usecase.AddressWaker) {
	db, err := database.NewGormDatabase()
	if err != nil {
		log.Fatalf("georef: database: %v", err)
	}
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Error)})
	for _, table := range []interface{}{&schema.GeoCEPPoint{}, &schema.GeoCity{}, &schema.GeoDistrictPoint{}, &schema.GeoCEPStreet{}, &schema.GeoReferenceLoad{}} {
		if !db.Migrator().HasTable(table) {
			log.Fatal("georef: the reference tables do not exist; start the server once so its migrations create them")
		}
	}
	return georef_repository.NewStore(db), lead_repository.NewGeocodeQueue(db, nil)
}

type dryStore struct{}

func (dryStore) UpsertCEPs(context.Context, []georef.CEPPoint, time.Time) error           { return nil }
func (dryStore) UpsertDistricts(context.Context, []georef.DistrictPoint, time.Time) error { return nil }
func (dryStore) UpsertCities(context.Context, []georef.City, time.Time) error             { return nil }
func (dryStore) ReplaceStreets(context.Context, string, []georef.Street, time.Time) error { return nil }
func (dryStore) RecordLoad(context.Context, georef.Load) error                            { return nil }
func (dryStore) WakeUnavailable(context.Context, time.Time) (int64, error)                { return 0, nil }
