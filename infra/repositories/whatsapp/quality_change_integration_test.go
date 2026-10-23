package whatsapp_repository

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/whatsapp/quality"
	"vozko/infra/database/schema"
	"vozko/infra/testenv"
)

func qualityIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testenv.Postgres(t).DSN()
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "quality_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&schema.WhatsAppQualityChange{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func redTemplate(at time.Time) quality.Change {
	return quality.Change{
		Subject: quality.SubjectTemplate, ExternalID: "2096955574574516", WABAID: "1453305666167883", Name: "refiliaxao_taxa_zero_correta",
		Signal: quality.SignalQuality, Previous: "YELLOW", Current: "RED", Source: quality.SourceWebhook, OccurredAt: at,
	}
}

func TestQualityChangeIntegration_ADuplicateDeliveryIsStoredOnce(t *testing.T) {
	db := qualityIntegrationDB(t)
	recorder := NewQualityChangeRepository(db)
	at := time.Date(2026, 10, 15, 13, 34, 38, 0, time.UTC)

	if err := recorder.Record(redTemplate(at)); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := recorder.Record(redTemplate(at)); err != nil {
		t.Fatalf("a repeated delivery is not an error: %v", err)
	}
	later := redTemplate(at.Add(time.Hour))
	if err := recorder.Record(later); err != nil {
		t.Fatalf("a later red is a new change: %v", err)
	}

	var rows []schema.WhatsAppQualityChange
	db.Order("occurred_at").Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].Current != "RED" || rows[0].Previous != "YELLOW" || !rows[0].OccurredAt.Equal(at) || rows[0].RecordedAt.IsZero() {
		t.Fatalf("stored %+v", rows[0])
	}
}

func TestQualityChangeIntegration_AnInvalidChangeWritesNothing(t *testing.T) {
	db := qualityIntegrationDB(t)
	recorder := NewQualityChangeRepository(db)
	invalid := redTemplate(time.Time{})

	err := recorder.Record(redTemplate(time.Now()), invalid)

	if !errors.Is(err, quality.ErrInvalidChange) {
		t.Fatalf("want ErrInvalidChange, got %v", err)
	}
	var count int64
	db.Model(&schema.WhatsAppQualityChange{}).Count(&count)
	if count != 0 {
		t.Fatalf("a batch with an invalid change writes nothing, found %d rows", count)
	}
}

func TestQualityChangeIntegration_TheTimelineKeepsTheLatestChangePerSignal(t *testing.T) {
	db := qualityIntegrationDB(t)
	repo := NewQualityChangeRepository(db)
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	yellow := redTemplate(at)
	yellow.Previous, yellow.Current = "GREEN", "YELLOW"
	paused := redTemplate(at.Add(30 * time.Minute))
	paused.Signal, paused.Previous, paused.Current = quality.SignalStatus, "APPROVED", "PAUSED"
	other := redTemplate(at.Add(2 * time.Hour))
	other.ExternalID = "another"
	if err := repo.Record(yellow, redTemplate(at.Add(time.Hour)), paused, other); err != nil {
		t.Fatalf("record: %v", err)
	}

	latest, err := repo.LatestBySignal(quality.SubjectTemplate, "2096955574574516")

	if err != nil {
		t.Fatalf("LatestBySignal: %v", err)
	}
	if q := latest[quality.SignalQuality]; q.Current != "RED" || !q.OccurredAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("quality = %+v, want the red change an hour later", q)
	}
	if s := latest[quality.SignalStatus]; s.Current != "PAUSED" || !s.OccurredAt.Equal(at.Add(30*time.Minute)) {
		t.Fatalf("status = %+v, want the paused change", s)
	}
	if len(latest) != 2 {
		t.Fatalf("got %d signals, want only this template's two", len(latest))
	}
}

func TestQualityChangeIntegration_RecentListsThisTemplatesChangesNewestFirst(t *testing.T) {
	db := qualityIntegrationDB(t)
	repo := NewQualityChangeRepository(db)
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	yellow := redTemplate(at)
	yellow.Previous, yellow.Current = "GREEN", "YELLOW"
	paused := redTemplate(at.Add(30 * time.Minute))
	paused.Signal, paused.Previous, paused.Current = quality.SignalStatus, "APPROVED", "PAUSED"
	other := redTemplate(at.Add(2 * time.Hour))
	other.ExternalID = "another"
	if err := repo.Record(yellow, redTemplate(at.Add(time.Hour)), paused, other); err != nil {
		t.Fatalf("record: %v", err)
	}

	recent, err := repo.Recent(quality.SubjectTemplate, "2096955574574516", 2)

	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 || recent[0].Current != "RED" || recent[1].Current != "PAUSED" {
		t.Fatalf("recent = %+v, want the red then the paused change", recent)
	}
}
