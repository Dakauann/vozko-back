package geocoding_repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/geocoding"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func platformDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "geocoding_platform")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Workspace{}, &schema.Lead{}, &schema.LeadAddress{},
		&schema.GeocodingSettings{}, &schema.GeocodingUsage{}, &schema.GeocodingUsageMonth{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestEveryTakenSlotIsKeptInTheMonthsHistoryAgainstPostgres(t *testing.T) {
	db := platformDB(t)
	store := NewUsageStore(db)
	ctx := context.Background()
	ws := uuid.NewString()
	settingsOf(t, db, ws, "opencage", 10)
	september := slot()
	september.CycleStart, september.NextCycle = september.CycleStart.AddDate(0, -1, 0), september.CycleStart
	september.Day, september.NextDay = september.CycleStart.AddDate(0, 0, 2), september.CycleStart.AddDate(0, 0, 3)
	for range 3 {
		if ok, _, err := store.TakeSlot(ctx, ws, september); err != nil || !ok {
			t.Fatalf("september slot = %v, %v", ok, err)
		}
	}
	october := slot()
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := store.TakeSlot(ctx, ws, october); err != nil {
				t.Errorf("october slot: %v", err)
			}
		}()
	}
	wg.Wait()
	history, err := store.History(ctx, []string{ws}, september.CycleStart)
	if err != nil {
		t.Fatal(err)
	}
	months := history[ws]
	if len(months) != 2 || !months[0].CycleStart.Equal(october.CycleStart) || months[0].Requests != 3 || months[1].Requests != 3 {
		t.Fatalf("History() = %+v, want October at its daily limit of 3 and September's 3 kept after the counter moved on", months)
	}
	usage, err := store.UsageOf(ctx, []string{ws, uuid.NewString()})
	if err != nil || len(usage) != 1 || usage[ws].Requests != 3 || !usage[ws].CycleStart.Equal(october.CycleStart) {
		t.Fatalf("UsageOf() = %+v, %v, want only the current counter", usage, err)
	}
}

func TestHistoryIncludesALiveCycleCountedBeforeTheHistoryExistedAgainstPostgres(t *testing.T) {
	db := platformDB(t)
	ws := uuid.NewString()
	cycle := slot().CycleStart
	if err := db.Exec("INSERT INTO geocoding_usage (workspace_id, cycle_start, requests, day, day_requests, updated_at) VALUES (?, ?, 41, ?, 2, now())", ws, cycle, cycle).Error; err != nil {
		t.Fatal(err)
	}
	history, err := NewUsageStore(db).History(context.Background(), []string{ws}, cycle.AddDate(0, -11, 0))
	if err != nil || len(history[ws]) != 1 || history[ws][0].Requests != 41 {
		t.Fatalf("History() = %+v, %v, want the live counter of the current cycle", history, err)
	}
}

func TestACycleCountedBeforeTheHistoryExistedIsKeptWhenTheNextCycleStartsAgainstPostgres(t *testing.T) {
	db := platformDB(t)
	store := NewUsageStore(db)
	ctx := context.Background()
	ws := uuid.NewString()
	settingsOf(t, db, ws, "opencage", 10)
	october := slot()
	september := october.CycleStart.AddDate(0, -1, 0)
	if err := db.Exec("INSERT INTO geocoding_usage (workspace_id, cycle_start, requests, day, day_requests, updated_at) VALUES (?, ?, 41, ?, 2, now())", ws, september, september).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if ok, _, err := store.TakeSlot(ctx, ws, october); err != nil || !ok {
			t.Fatalf("october slot = %v, %v", ok, err)
		}
	}
	history, err := store.History(ctx, []string{ws}, september)
	if err != nil {
		t.Fatal(err)
	}
	months := history[ws]
	if len(months) != 2 || !months[0].CycleStart.Equal(october.CycleStart) || months[0].Requests != 2 || !months[1].CycleStart.Equal(september) || months[1].Requests != 41 {
		t.Fatalf("History() = %+v, want October's 2 and the 41 September requests counted before the history existed", months)
	}
}

func TestPlatformReaderListsAndCountsWorkspacesAgainstPostgres(t *testing.T) {
	db := platformDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	cycle := slot().CycleStart
	workspace := func(name string) string {
		id := uuid.NewString()
		if err := db.Exec("INSERT INTO workspaces (id, owner_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", id, uuid.NewString(), name, now, now).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	lead := func(ws string, deleted bool) string {
		id := uuid.NewString()
		var deletedAt any
		if deleted {
			deletedAt = now
		}
		if err := db.Exec("INSERT INTO leads (id, workspace_id, name, created_at, updated_at, deleted_at) VALUES (?, ?, 'Lead', ?, ?, ?)", id, ws, now, now, deletedAt).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	addressOf := func(ws, leadID string, lat *float64, precision, status string) {
		if err := db.Exec(`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, latitude, longitude, geo_precision, geo_status, fingerprint, created_at, updated_at)
			VALUES (?, ?, ?, 'home', true, 0, ?, ?, NULLIF(?, ''), ?, 'f', ?, ?)`, uuid.NewString(), ws, leadID, lat, lat, precision, status, now, now).Error; err != nil {
			t.Fatal(err)
		}
	}
	lat := -23.5

	school := workspace("Escola 50%")
	clinic := workspace("Clínica")
	quiet := workspace("Sem leads")
	_ = workspace("Apagado")
	if err := db.Exec("UPDATE workspaces SET deleted_at = now() WHERE name = 'Apagado'").Error; err != nil {
		t.Fatal(err)
	}
	settingsOf(t, db, quiet, "opencage", 100)
	if err := db.Exec("INSERT INTO geocoding_usage (workspace_id, cycle_start, requests, day, day_requests, updated_at) VALUES (?, ?, 9, ?, 1, now())", clinic, cycle, cycle).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		addressOf(school, lead(school, false), &lat, "exact", "located")
	}
	addressOf(school, lead(school, false), &lat, "district", "approximate")
	addressOf(school, lead(school, false), nil, "", "pending")
	lead(school, false)
	addressOf(school, lead(school, true), &lat, "exact", "located")
	lead(clinic, false)
	_ = workspace("Nada")

	reader := NewPlatformReader(db)
	got, total, err := reader.GeocodingWorkspaces(ctx, geocoding.PlatformQuery{Page: 1, PageSize: 10}, cycle)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(got) != 3 || got[0].ID != clinic || got[1].Name != "Escola 50%" || got[2].ID != quiet {
		t.Fatalf("GeocodingWorkspaces() = %+v, %d, want the clinic first by usage, then by name, without deleted or idle workspaces", got, total)
	}
	searched, total, err := reader.GeocodingWorkspaces(ctx, geocoding.PlatformQuery{Page: 1, PageSize: 10, Search: "50%"}, cycle)
	if err != nil || total != 1 || len(searched) != 1 || searched[0].ID != school {
		t.Fatalf("search = %+v, %d, %v, want the literal percent sign matched", searched, total, err)
	}
	beyond, total, err := reader.GeocodingWorkspaces(ctx, geocoding.PlatformQuery{Page: 4, PageSize: 1}, cycle)
	if err != nil || total != 3 || len(beyond) != 0 {
		t.Fatalf("a page past the end = %+v, %d, %v", beyond, total, err)
	}

	coverage, err := reader.Coverage(ctx, []string{school, clinic, quiet})
	if err != nil {
		t.Fatal(err)
	}
	c := coverage[school]
	if c.Total != 5 || c.WithoutAddress != 1 || c.OnMap != 2 || c.Approximate != 1 || c.Pending != 1 {
		t.Fatalf("school coverage = %+v, want 5 live leads, 1 without address, 2 on the map, 1 approximate, 1 pending", c)
	}
	if coverage[clinic].Total != 1 || coverage[clinic].WithoutAddress != 1 {
		t.Fatalf("clinic coverage = %+v", coverage[clinic])
	}
	if _, ok := coverage[quiet]; ok {
		t.Fatal("a workspace without leads got a coverage row")
	}
}
