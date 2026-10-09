package cep_repository

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/cep"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func TestSaveUpsertsSoTwoConcurrentLookupsNeverCollide(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	if strings.Count(upsertSQL, "?") != 8 {
		t.Fatalf("upsert must bind exactly 8 values, has %d placeholders", strings.Count(upsertSQL, "?"))
	}
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO ceps (id, cep, logradouro, complement, bairro, localidade, uf, ibge, created_at, updated_at)`)+
		`.*`+regexp.QuoteMeta(`ON CONFLICT (cep) DO UPDATE SET`)+`.*`+regexp.QuoteMeta(`ibge = EXCLUDED.ibge`)+`.*`+regexp.QuoteMeta(`deleted_at = NULL`)).
		WithArgs(sqlmock.AnyArg(), "01310100", "Avenida Paulista", "lado par", "Bela Vista", "São Paulo", "SP", "3550308").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := NewRepository(db).Save(&cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Complement: "lado par", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveStoresAMissingCityCodeAsNull(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO ceps`).
		WithArgs(sqlmock.AnyArg(), "01310100", "", "", "", "São Paulo", "SP", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).Save(&cep.CEPInfo{Cep: "01310100", Localidade: "São Paulo", Uf: "SP"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetByCodeReadsTheCityCode(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	checkedAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT \* FROM "ceps" WHERE cep = \$1 AND "ceps"."deleted_at" IS NULL`).
		WithArgs("01310100", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cep", "logradouro", "complement", "bairro", "localidade", "uf", "ibge", "updated_at"}).
			AddRow("id-1", "01310100", "Avenida Paulista", "", "Bela Vista", "São Paulo", "SP", "3550308", checkedAt))

	info, err := NewRepository(db).GetByCode("01310100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308", CheckedAt: checkedAt}
	if info == nil || *info != expected {
		t.Fatalf("GetByCode() = %+v, expected %+v", info, expected)
	}
}

func TestGetByCodeReturnsNothingForAnUncachedCEP(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "ceps"`).WithArgs("01310100", 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	info, err := NewRepository(db).GetByCode("01310100")
	if err != nil || info != nil {
		t.Fatalf("expected no row and no error, got %+v, %v", info, err)
	}
}

func TestMarkCheckedRecordsTheAttemptOnTheRow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	if strings.Count(markCheckedSQL, "?") != 1 {
		t.Fatalf("mark must bind exactly the code, has %d placeholders", strings.Count(markCheckedSQL, "?"))
	}
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE ceps SET updated_at = now() WHERE cep = `)).
		WithArgs("01310100").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).MarkChecked("01310100"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
