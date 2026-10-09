package cep_repository

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/cep"
	"vozko/infra/database/schema"
)

func integrationDB(t *testing.T) *gorm.DB {
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
	schemaName := "cep_test_" + uuid.New().String()[:8]
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
	if err := db.Exec(`CREATE TABLE ceps (id uuid PRIMARY KEY, cep varchar(10) NOT NULL, logradouro varchar(200), complement varchar(100),
		bairro varchar(100), localidade varchar(100), uf varchar(2), created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`).Error; err != nil {
		t.Fatalf("create legacy ceps: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_ceps_cep ON ceps (cep)`).Error; err != nil {
		t.Fatalf("create legacy index: %v", err)
	}
	if err := db.Exec(`INSERT INTO ceps (id, cep, localidade, uf) VALUES (?, '01310100', 'São Paulo', 'SP')`, uuid.New().String()).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := db.AutoMigrate(&schema.CEP{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestIntegrationLegacyRowsGainANullCityCodeThatALookupFills(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	legacy, err := repo.GetByCode("01310100")
	if err != nil || legacy == nil || legacy.IBGE != "" {
		t.Fatalf("expected the legacy row without a city code, got %+v, %v", legacy, err)
	}

	refreshed := &cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.Save(refreshed)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent saves of one CEP must all succeed: %v", err)
		}
	}

	got, err := repo.GetByCode("01310100")
	if err != nil || got == nil || got.CheckedAt.IsZero() {
		t.Fatalf("GetByCode() = %+v, %v; expected a checked row", got, err)
	}
	saved := *got
	saved.CheckedAt = refreshed.CheckedAt
	if saved != *refreshed {
		t.Fatalf("GetByCode() = %+v; expected %+v", got, refreshed)
	}
	if err := repo.MarkChecked("01310100"); err != nil {
		t.Fatalf("MarkChecked: %v", err)
	}
	if marked, err := repo.GetByCode("01310100"); err != nil || marked.CheckedAt.Before(got.CheckedAt) || marked.IBGE != "3550308" {
		t.Fatalf("MarkChecked must only move the check time forward, got %+v, %v", marked, err)
	}
	var rows int64
	if err := db.Table("ceps").Count(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("expected one row, got %d, %v", rows, err)
	}
}
