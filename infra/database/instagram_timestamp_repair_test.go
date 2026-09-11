package database

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepairInstagramSecondTimestampsRepairsBothProjections(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE instagram_comments")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE audience_analyses")).WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repairInstagramSecondTimestamps(db); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepairInstagramSecondTimestampsIsRegistered(t *testing.T) {
	for _, name := range repairNames() {
		if name == "ig_repair_second_timestamps" {
			return
		}
	}
	t.Fatal("the Instagram timestamp repair is not registered in runDataRepairs")
}
