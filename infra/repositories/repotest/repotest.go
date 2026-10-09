package repotest

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func dsn() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
}

type Options struct {
	TimeZone    string
	PrepareStmt bool
}

func (o Options) dsnParams() string {
	if o.TimeZone == "" {
		return ""
	}
	return " timezone=" + o.TimeZone
}

func IsolatedDB(t testing.TB, prefix string, models ...interface{}) *gorm.DB {
	t.Helper()
	return IsolatedDBWith(t, prefix, Options{}, models...)
}

func IsolatedDBWith(t testing.TB, prefix string, opts Options, models ...interface{}) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn()), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := prefix + "_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn()+" search_path="+schemaName+opts.dsnParams()), &gorm.Config{Logger: silent.Logger, PrepareStmt: opts.PrepareStmt})
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
	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return db
}
