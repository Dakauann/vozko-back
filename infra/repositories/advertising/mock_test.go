package advertising_repository

import (
	"bytes"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
)

func withPII(t *testing.T) {
	t.Helper()
	v, err := vault.New(bytes.Repeat([]byte{0x11}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	piigorm.SetService(s)
	t.Cleanup(func() { piigorm.SetService(nil) })
}

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	withPII(t)
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func expectationsMet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
