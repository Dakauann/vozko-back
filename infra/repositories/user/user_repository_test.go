package user_repository

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

var piiTestKey = strings.Repeat("A", 32)

var testPIISvc *pii.Service

func TestMain(m *testing.M) {
	enc := base64.StdEncoding.EncodeToString([]byte(piiTestKey))
	svc, err := pii.LoadFromEnviron(
		[]string{
			"VOZKO_PII_KEK_V1=" + enc,
			"VOZKO_PII_BLIND_INDEX_KEY=" + enc,
		},
		func(k string) string {
			switch k {
			case "VOZKO_PII_KEK_V1", "VOZKO_PII_BLIND_INDEX_KEY":
				return enc
			}
			return ""
		},
	)
	if err != nil {
		panic("test PII service init failed: " + err.Error())
	}
	testPIISvc = svc
	piigorm.SetService(svc)
	code := m.Run()
	piigorm.SetService(nil)
	if code != 0 {
		panic("tests failed")
	}
}

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func encrypted(t *testing.T, plain string) []byte {
	t.Helper()
	v, err := piigorm.NewEncrypted(plain).Value()
	if err != nil {
		t.Fatalf("encrypt %q: %v", plain, err)
	}
	if v == nil {
		return nil
	}
	return v.([]byte)
}

func blindIdx(t *testing.T, scope, value string) []byte {
	t.Helper()
	bi, err := piigorm.NewBlindIndex(scope, value)
	if err != nil {
		t.Fatalf("blind index %s/%s: %v", scope, value, err)
	}
	return []byte(bi)
}

func TestEncryptDoc_EmptyReturnsNullAndNilIndex(t *testing.T) {
	enc, bi, err := encryptDoc(schema.UserCPFBlindScope, "")
	if err != nil {
		t.Fatal(err)
	}
	if enc.Valid {
		t.Fatalf("expected NULL encrypted value, got %+v", enc)
	}
	if bi != nil {
		t.Fatalf("expected nil blind index, got %v", bi)
	}
}

func TestEncryptCPF_NonEmpty(t *testing.T) {
	enc, bi, err := encryptCPF("12345678901")
	if err != nil {
		t.Fatal(err)
	}
	if !enc.Valid || enc.Plain != "12345678901" {
		t.Fatalf("unexpected encrypted value: %+v", enc)
	}
	if len(bi) == 0 {
		t.Fatal("expected non-empty blind index")
	}
}

func TestEncryptCNPJ_NonEmpty(t *testing.T) {
	enc, bi, err := encryptCNPJ("12345678000199")
	if err != nil {
		t.Fatal(err)
	}
	if !enc.Valid || enc.Plain != "12345678000199" {
		t.Fatalf("unexpected encrypted value: %+v", enc)
	}
	if len(bi) == 0 {
		t.Fatal("expected non-empty blind index")
	}
}

func TestEncryptDoc_BlindIndexError(t *testing.T) {

	prev := testPIISvc
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(prev) })

	_, _, err := encryptDoc(schema.UserCPFBlindScope, "12345678901")
	if err == nil {
		t.Fatal("expected error when PII service is not configured")
	}
}

func TestCreate_PersistsEncryptedFields(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()

	mock.ExpectExec(`INSERT INTO "users"`).WillReturnResult(sqlmock.NewResult(1, 1))

	repo := NewUserRepository(db)
	u := &user.User{
		ID: "u1", Username: "Alice", Email: "a@x.io", Password: "h",
		Role: user.RoleUser, CustomerType: user.CustomerTypeIndividual,
		CPF: "12345678901", CNPJ: "12345678000199",
	}
	if err := repo.Create(u); err != nil {
		t.Fatal(err)
	}
	if u.CPF != "12345678901" || u.CNPJ != "12345678000199" {
		t.Fatalf("round-trip lost values: %+v", u)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreate_EmptyDocumentsBecomeNull(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`INSERT INTO "users"`).WillReturnResult(sqlmock.NewResult(1, 1))

	repo := NewUserRepository(db)
	u := &user.User{ID: "u2", Username: "B", Email: "b@x.io", Password: "h", Role: user.RoleUser}
	if err := repo.Create(u); err != nil {
		t.Fatal(err)
	}
	if u.CPF != "" || u.CNPJ != "" {
		t.Fatalf("expected empty CPF/CNPJ, got %+v", u)
	}
}

func TestCreate_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`INSERT INTO "users"`).WillReturnError(errors.New("boom"))

	repo := NewUserRepository(db)
	err := repo.Create(&user.User{ID: "u3", Username: "C", Email: "c@x.io", Password: "h"})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestCreate_EncryptCPFError(t *testing.T) {
	prev := testPIISvc
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(prev) })

	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewUserRepository(db)
	if err := repo.Create(&user.User{ID: "u", CPF: "111"}); err == nil {
		t.Fatal("expected encryption error")
	}
}

func TestCreate_EncryptCNPJError(t *testing.T) {
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewUserRepository(db)

	prev := testPIISvc
	t.Cleanup(func() { piigorm.SetService(prev) })
	piigorm.SetService(nil)
	if err := repo.Create(&user.User{ID: "u", CNPJ: "222"}); err == nil {
		t.Fatal("expected encryption error")
	}
}

func TestFindByEmail_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	rows := sqlmock.NewRows([]string{"id", "email", "cpf", "cnpj"}).
		AddRow("u1", "a@x.io", encrypted(t, "12345678901"), nil)
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE email`).WillReturnRows(rows)

	repo := NewUserRepository(db)
	got, err := repo.FindByEmail("a@x.io")
	if err != nil {
		t.Fatal(err)
	}
	if got.CPF != "12345678901" || got.CNPJ != "" {
		t.Fatalf("decrypt mismatch: %+v", got)
	}
}

func TestFindByEmail_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE email`).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewUserRepository(db)
	_, err := repo.FindByEmail("missing@x.io")
	if !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFindByEmail_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE email`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.FindByEmail("x@x.io"); err == nil || errors.Is(err, user.ErrNotFound) {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestFindByDocument_EmptyReturnsNotFound(t *testing.T) {
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewUserRepository(db)
	if _, err := repo.FindByDocument(""); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFindByDocument_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	rows := sqlmock.NewRows([]string{"id", "email", "cpf", "cnpj"}).
		AddRow("u1", "a@x.io", encrypted(t, "12345678901"), nil)
	mock.ExpectQuery(`cpf_blind = .+ OR cnpj_blind`).WillReturnRows(rows)

	repo := NewUserRepository(db)
	got, err := repo.FindByDocument("12345678901")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "u1" || got.CPF != "12345678901" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestFindByDocument_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind = .+ OR cnpj_blind`).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewUserRepository(db)
	if _, err := repo.FindByDocument("999"); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFindByDocument_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind = .+ OR cnpj_blind`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.FindByDocument("999"); err == nil || errors.Is(err, user.ErrNotFound) {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestFindByDocument_CPFBlindError(t *testing.T) {
	prev := testPIISvc
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(prev) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewUserRepository(db)
	if _, err := repo.FindByDocument("x"); err == nil {
		t.Fatal("expected error when PII service missing")
	}
}

func TestFindByID_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	rows := sqlmock.NewRows([]string{"id", "email"}).AddRow("u1", "a@x.io")
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE id`).WillReturnRows(rows)
	repo := NewUserRepository(db)
	got, err := repo.FindByID("u1")
	if err != nil || got.ID != "u1" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestFindByID_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE id`).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewUserRepository(db)
	if _, err := repo.FindByID("u1"); !errors.Is(err, user.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestFindByID_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE id`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.FindByID("u1"); err == nil || errors.Is(err, user.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestFindByIDs_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	rows := sqlmock.NewRows([]string{"id", "email"}).AddRow("u1", "a").AddRow("u2", "b")
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE id IN`).WillReturnRows(rows)
	repo := NewUserRepository(db)
	got, err := repo.FindByIDs([]string{"u1", "u2"})
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestFindByIDs_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE id IN`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.FindByIDs([]string{"u1"}); err == nil {
		t.Fatal()
	}
}

func TestUpdate_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "users" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewUserRepository(db)
	err := repo.Update("u1", &user.User{Email: "x@x.io", CPF: "12345678901"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdate_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "users" SET`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if err := repo.Update("u1", &user.User{Email: "x@x.io"}); err == nil {
		t.Fatal()
	}
}

func TestUpdate_EncryptCPFError(t *testing.T) {
	prev := testPIISvc
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(prev) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewUserRepository(db)
	if err := repo.Update("u1", &user.User{CPF: "111"}); err == nil {
		t.Fatal()
	}
}

func TestUpdate_EncryptCNPJError(t *testing.T) {
	prev := testPIISvc
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(prev) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewUserRepository(db)
	if err := repo.Update("u1", &user.User{CNPJ: "222"}); err == nil {
		t.Fatal()
	}
}

func TestDelete_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`DELETE FROM "users"`).WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewUserRepository(db)
	if err := repo.Delete("u1"); err != nil {
		t.Fatal(err)
	}
}

func TestGetUserRole_DefaultsToUser(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .*"role".* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow(""))
	repo := NewUserRepository(db)
	r, err := repo.GetUserRole("u1")
	if err != nil || r != string(user.RoleUser) {
		t.Fatalf("%v %s", err, r)
	}
}

func TestGetUserRole_ReturnsStored(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .*"role".* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("admin"))
	repo := NewUserRepository(db)
	r, err := repo.GetUserRole("u1")
	if err != nil || r != "admin" {
		t.Fatalf("%v %s", err, r)
	}
}

func TestGetUserRole_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.GetUserRole("u1"); err == nil {
		t.Fatal()
	}
}

func TestGetTokenVersion_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .*"token_version".* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"token_version"}).AddRow(7))
	repo := NewUserRepository(db)
	v, err := repo.GetTokenVersion("u1")
	if err != nil || v != 7 {
		t.Fatalf("%v %d", err, v)
	}
}

func TestIncrementTokenVersion_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "users" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .*"token_version".* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"token_version"}).AddRow(8))
	repo := NewUserRepository(db)
	v, err := repo.IncrementTokenVersion("u1")
	if err != nil || v != 8 {
		t.Fatalf("%v %d", err, v)
	}
}

func TestIncrementTokenVersion_UpdateError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "users" SET`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.IncrementTokenVersion("u1"); err == nil {
		t.Fatal()
	}
}

func TestList_NoFilters(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email"}).AddRow("u1", "a@x.io"))

	repo := NewUserRepository(db)
	page, err := repo.List(user.ListUsersInput{QueryOptions: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.TotalItems != 1 {
		t.Fatalf("%+v", page)
	}
}

func TestList_WithSearchAndRole(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	repo := NewUserRepository(db)
	role := user.RoleAdmin
	if _, err := repo.List(user.ListUsersInput{
		QueryOptions: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 10}},
		Search:       "ALICE",
		Role:         &role,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestList_CountError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.List(user.ListUsersInput{}); err == nil {
		t.Fatal()
	}
}

func TestList_FindError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(5))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.List(user.ListUsersInput{}); err == nil {
		t.Fatal()
	}
}

func TestCountByRole_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(3))
	repo := NewUserRepository(db)
	n, err := repo.CountByRole(user.RoleAdmin)
	if err != nil || n != 3 {
		t.Fatalf("%v %d", err, n)
	}
}

func TestCountByRole_Error(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT count`).WillReturnError(errors.New("db"))
	repo := NewUserRepository(db)
	if _, err := repo.CountByRole(user.RoleAdmin); err == nil {
		t.Fatal()
	}
}

func TestWithTx_ReturnsNewRepoWithSameDB(t *testing.T) {
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewUserRepository(db).(*UserRepositoryImpl)
	tx := repo.WithTx(db)
	if tx == nil {
		t.Fatal("nil tx repo")
	}
}
