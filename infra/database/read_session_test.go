package database

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

func TestReadSessionSettingsBoundTheStatement(t *testing.T) {
	got := ReadSessionSettings("32MB", "5s")
	want := []string{"SET LOCAL work_mem = '32MB'", "SET LOCAL jit = off", "SET LOCAL statement_timeout = '5s'"}
	if len(got) != len(want) {
		t.Fatalf("ReadSessionSettings() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ReadSessionSettings() = %q, want %q", got, want)
		}
	}
}

func TestInReadSessionAppliesTheSettingsInsideOneReadOnlyTransaction(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`SET LOCAL work_mem = '32MB'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SET LOCAL jit = off`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SET LOCAL statement_timeout = '5s'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT 1`).WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	mock.ExpectCommit()
	var one int
	err := InReadSession(context.Background(), db, ReadSessionSettings("32MB", "5s"), func(tx *gorm.DB) error {
		return tx.Raw("SELECT 1").Scan(&one).Error
	})
	if err != nil || one != 1 {
		t.Fatalf("InReadSession() = %d, %v", one, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInReadSessionReportsAStatementTimeoutAsADeadline(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT 1`).WillReturnError(errors.New("ERROR: canceling statement due to statement timeout (SQLSTATE 57014)"))
	mock.ExpectRollback()
	err := InReadSession(context.Background(), db, nil, func(tx *gorm.DB) error {
		var one int
		return tx.Raw("SELECT 1").Scan(&one).Error
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("InReadSession() = %v, want a deadline", err)
	}
}

func TestInReadSessionStopsAtAFailedSetting(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()
	boom := errors.New("boom")
	mock.ExpectBegin()
	mock.ExpectExec(`SET LOCAL jit = off`).WillReturnError(boom)
	mock.ExpectRollback()
	called := false
	err := InReadSession(context.Background(), db, []string{"SET LOCAL jit = off"}, func(*gorm.DB) error {
		called = true
		return nil
	})
	if !errors.Is(err, boom) || called {
		t.Fatalf("InReadSession() = %v, called = %v, want the setting error and no read", err, called)
	}
}
