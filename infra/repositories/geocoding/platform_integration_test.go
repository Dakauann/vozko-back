package geocoding_repository

import (
	"context"
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
	if err := db.AutoMigrate(&schema.Workspace{}, &schema.Lead{}, &schema.LeadAddress{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestPlatformReaderListsAndCountsWorkspacesAgainstPostgres(t *testing.T) {
	db := platformDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
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
	got, total, err := reader.GeocodingWorkspaces(ctx, geocoding.PlatformQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(got) != 2 || got[0].ID != clinic || got[1].ID != school {
		t.Fatalf("GeocodingWorkspaces() = %+v, %d, want the workspaces with live leads by name, without deleted or empty workspaces", got, total)
	}
	searched, total, err := reader.GeocodingWorkspaces(ctx, geocoding.PlatformQuery{Page: 1, PageSize: 10, Search: "50%"})
	if err != nil || total != 1 || len(searched) != 1 || searched[0].ID != school {
		t.Fatalf("search = %+v, %d, %v, want the literal percent sign matched", searched, total, err)
	}
	beyond, total, err := reader.GeocodingWorkspaces(ctx, geocoding.PlatformQuery{Page: 4, PageSize: 1})
	if err != nil || total != 2 || len(beyond) != 0 {
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
