package workflow_repository

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/infra/testenv"
)

func postgresSchemaTx(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	dsn := testenv.Postgres(t).DSN()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	t.Cleanup(func() {
		tx.Rollback()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	schemaName := "wf_test_" + uuid.NewString()[:8]
	for _, stmt := range []string{"CREATE SCHEMA " + schemaName, "SET LOCAL search_path TO " + schemaName} {
		if err := tx.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if len(models) > 0 {
		if err := tx.AutoMigrate(models...); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return tx
}
