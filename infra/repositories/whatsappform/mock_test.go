package whatsappform_repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ctx = context.Background()

const (
	wsID      = "0b0e7c1a-0000-4000-8000-000000000001"
	otherWsID = "0b0e7c1a-0000-4000-8000-000000000002"
	formID    = "0b0e7c1a-0000-4000-8000-0000000000f1"
	versionID = "0b0e7c1a-0000-4000-8000-0000000000a1"
	nextID    = "0b0e7c1a-0000-4000-8000-0000000000a2"
	phoneID   = "0b0e7c1a-0000-4000-8000-0000000000b1"
	sendID    = "0b0e7c1a-0000-4000-8000-0000000000c1"
	respID    = "0b0e7c1a-0000-4000-8000-0000000000d1"
	pubID     = "0b0e7c1a-0000-4000-8000-0000000000e1"
	userID    = "0b0e7c1a-0000-4000-8000-000000000a01"
	deptID    = "0b0e7c1a-0000-4000-8000-000000000d01"
	campID    = "0b0e7c1a-0000-4000-8000-000000000c01"
)

var at = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func exactly(sql string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(sql), `\?`, `\$[0-9]+`) + "$"
}

func expectationsMet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func placeholdersMatch(t *testing.T, sql string, args []interface{}) {
	t.Helper()
	if got := strings.Count(sql, "?"); got != len(args) {
		t.Fatalf("%d placeholders but %d arguments in %s", got, len(args), sql)
	}
}

func uniqueViolation(constraint string) error {
	return errors.New(`ERROR: duplicate key value violates unique constraint "` + constraint + `" (SQLSTATE 23505)`)
}
