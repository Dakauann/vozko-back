package whatsapp_repository

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/whatsapp/quality"
	"vozko/infra/database/schema"
)

func qualityIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
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
