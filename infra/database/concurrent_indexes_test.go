package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConcurrentIndexBuildsWhenNothingIsThere(t *testing.T) {
	db, mock, sqlDB := sqlmockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_x").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := createIndexConcurrently(db, ddl{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConcurrentIndexReplacesAnInvalidLeftover(t *testing.T) {
	db, mock, sqlDB := sqlmockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_x").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`DROP INDEX CONCURRENTLY IF EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := createIndexConcurrently(db, ddl{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConcurrentIndexReportsAFailedValidityCheck(t *testing.T) {
	db, mock, sqlDB := sqlmockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WillReturnError(errors.New("catalog unavailable"))

	if err := createIndexConcurrently(db, ddl{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err == nil {
		t.Fatalf("createIndexConcurrently() built an index without knowing whether an invalid one was in the way")
	}
}

func TestEveryConcurrentIndexIsBuiltConcurrently(t *testing.T) {
	for _, idx := range concurrentIndexes() {
		sql := strings.Join(strings.Fields(idx.sql), " ")
		plain := strings.Contains(sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+idx.name+" ON ")
		unique := strings.Contains(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+idx.name+" ON ")
		if !plain && !unique {
			t.Fatalf("%s is not built concurrently under its own name: %s", idx.name, idx.sql)
		}
	}
}

