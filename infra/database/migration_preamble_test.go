package database

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"vozko/infra/repositories/repotest"
)

func TestTheMigrationWaitsItsTurnThenRefusesToHangOnATableLock(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SELECT set_config\('lock_timeout', \$1, true\)`).WithArgs(migrationLockTimeout).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS vector`).WillReturnResult(sqlmock.NewResult(0, 0))
	expectSearchFold(mock)

	if err := prepareMigration(db); err != nil {
		t.Fatalf("prepareMigration: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAMissingExtensionStopsTheMigration(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SELECT set_config\('lock_timeout'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS vector`).WillReturnError(errors.New("extension is not available"))

	if err := prepareMigration(db); err == nil {
		t.Fatal("a missing extension must stop the boot migration")
	}
}

func expectSearchFold(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA public`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS btree_gin WITH SCHEMA public`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE OR REPLACE FUNCTION vozko_fold\(text\) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT`).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestAMissingSearchExtensionStopsTheMigration(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SELECT set_config\('lock_timeout'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS vector`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS unaccent`).WillReturnError(errors.New("extension \"unaccent\" is not available"))

	if err := prepareMigration(db); err == nil {
		t.Fatal("a missing search extension must stop the boot migration")
	}
}

func TestASecondBootWaitsLongerThanTheLockTimeoutForTheFirstAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "migration_lock")
	const shortTimeout = "500ms"
	held := 2 * time.Second

	first := db.Begin()
	if err := serializeMigration(first, shortTimeout); err != nil {
		first.Rollback()
		t.Fatalf("first boot: %v", err)
	}

	second := make(chan error, 1)
	go func() {
		second <- db.Transaction(func(tx *gorm.DB) error {
			if err := serializeMigration(tx, shortTimeout); err != nil {
				return err
			}
			var timeout string
			if err := tx.Raw(`SHOW lock_timeout`).Scan(&timeout).Error; err != nil {
				return err
			}
			if timeout != shortTimeout {
				return errors.New("the lock timeout must apply once the turn is taken, got " + timeout)
			}
			return nil
		})
	}()

	time.Sleep(held)
	if err := first.Commit().Error; err != nil {
		t.Fatalf("first commit: %v", err)
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("the second boot must wait its turn, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second boot never got the migration lock")
	}
}
