package database

import (
	"fmt"
	"testing"
	"time"

	"vozko/infra/testenv"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTestPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testenv.Postgres(t).DSN()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestAnExistingIndexDoesNotWaitForTheTableLockAtStartup(t *testing.T) {
	setup := openTestPostgres(t)
	table := fmt.Sprintf("ddl_present_%d", time.Now().UnixNano())
	if err := setup.Exec("CREATE TABLE " + table + " (id int, code text)").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setup.Exec("DROP TABLE IF EXISTS " + table) })
	index := ddl{name: "ux_" + table, sql: "CREATE UNIQUE INDEX IF NOT EXISTS ux_" + table + " ON " + table + " (code)"}
	if err := execDDL(setup, []ddl{index}); err != nil {
		t.Fatalf("first creation: %v", err)
	}

	holder := openTestPostgres(t).Begin()
	defer holder.Rollback()
	if err := holder.Exec("LOCK TABLE " + table + " IN SHARE UPDATE EXCLUSIVE MODE").Error; err != nil {
		t.Fatal(err)
	}
	startup := openTestPostgres(t).Begin()
	defer startup.Rollback()
	if err := startup.Exec("SET LOCAL lock_timeout = '1s'").Error; err != nil {
		t.Fatal(err)
	}

	if err := execDDL(startup, []ddl{index}); err != nil {
		t.Fatalf("an index that is already there must not wait for the table lock: %v", err)
	}
}
