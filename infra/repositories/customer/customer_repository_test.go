package customer_repository

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/customer"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

var (
	piiTestKey = strings.Repeat("B", 32)
	testPIISvc *pii.Service
)

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

func encryptedBytes(t *testing.T, plain string) []byte {
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

func TestEncryptCustomerDoc_EmptyReturnsNull(t *testing.T) {
	enc, bi, err := encryptCustomerDoc(schema.CustomerCPFBlindScope, "")
	if err != nil {
		t.Fatal(err)
	}
	if enc.Valid || bi != nil {
		t.Fatalf("unexpected: %+v %v", enc, bi)
	}
}

func TestEncryptCustomerDoc_NonEmpty(t *testing.T) {
	enc, bi, err := encryptCustomerDoc(schema.CustomerCPFBlindScope, "111")
	if err != nil {
		t.Fatal(err)
	}
	if !enc.Valid || enc.Plain != "111" || len(bi) == 0 {
		t.Fatalf("unexpected: %+v %v", enc, bi)
	}
}

func TestEncryptCustomerDoc_Error(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	if _, _, err := encryptCustomerDoc(schema.CustomerCPFBlindScope, "111"); err == nil {
		t.Fatal()
	}
}

func TestBuildSchemaCustomer_CPFError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	if _, err := buildSchemaCustomer(&customer.Customer{CPF: "111"}); err == nil {
		t.Fatal()
	}
}

func TestBuildSchemaCustomer_CNPJError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	if _, err := buildSchemaCustomer(&customer.Customer{CNPJ: "111"}); err == nil {
		t.Fatal()
	}
}

func TestNullStringHelpers(t *testing.T) {
	if ns := toNullString(""); ns.Valid {
		t.Fatal()
	}
	if ns := toNullString("x"); !ns.Valid || ns.String != "x" {
		t.Fatal()
	}
	if s := fromNullString(sql.NullString{}); s != "" {
		t.Fatal()
	}
	if s := fromNullString(sql.NullString{String: "y", Valid: true}); s != "y" {
		t.Fatal()
	}
}

func TestCreateCustomer_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`INSERT INTO "customers"`).WillReturnResult(sqlmock.NewResult(1, 1))
	repo := NewCustomerRepository(db)
	c := &customer.Customer{ID: "c1", Name: "Alice", Email: "a@x.io", CPF: "111"}
	if err := repo.CreateCustomer(c); err != nil {
		t.Fatal(err)
	}
	if c.ID != "c1" {
		t.Fatal("id lost")
	}
}

func TestCreateCustomer_EncryptError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewCustomerRepository(db)
	if err := repo.CreateCustomer(&customer.Customer{CPF: "111"}); err == nil {
		t.Fatal()
	}
}

func TestCreateCustomer_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`INSERT INTO "customers"`).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if err := repo.CreateCustomer(&customer.Customer{ID: "c"}); err == nil {
		t.Fatal()
	}
}

func TestGetCustomerByID_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	rows := sqlmock.NewRows([]string{"id", "email", "cpf"}).AddRow("c1", "a@x.io", encryptedBytes(t, "111"))
	mock.ExpectQuery(`SELECT .* FROM "customers" WHERE id`).WillReturnRows(rows)
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByID("c1")
	if err != nil || got == nil || got.CPF != "111" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByID_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "customers" WHERE id`).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByID("missing")
	if err != nil || got != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByID_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "customers" WHERE id`).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if _, err := repo.GetCustomerByID("c1"); err == nil {
		t.Fatal()
	}
}

func TestGetCustomerByDocument_EmptyReturnsNil(t *testing.T) {
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocument("")
	if err != nil || got != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocument_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	rows := sqlmock.NewRows([]string{"id"}).AddRow("c1")
	mock.ExpectQuery(`cpf_blind = .+ OR cnpj_blind`).WillReturnRows(rows)
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocument("111")
	if err != nil || got == nil || got.ID != "c1" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocument_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind`).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocument("111")
	if err != nil || got != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocument_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind`).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if _, err := repo.GetCustomerByDocument("111"); err == nil {
		t.Fatal()
	}
}

func TestGetCustomerByDocument_EncryptError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewCustomerRepository(db)
	if _, err := repo.GetCustomerByDocument("111"); err == nil {
		t.Fatal()
	}
}

func TestGetCustomerByDocumentEmailOrPhone_AllEmpty(t *testing.T) {
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocumentEmailOrPhone("", "", "")
	if err != nil || got != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocumentEmailOrPhone_DocOnly(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocumentEmailOrPhone("111", "", "")
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocumentEmailOrPhone_DocAndEmailAndPhone(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocumentEmailOrPhone("111", "a@x.io", "+5511")
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocumentEmailOrPhone_EmailOnly(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`email`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocumentEmailOrPhone("", "a@x.io", "")
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocumentEmailOrPhone_PhoneOnly(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`mobile_number`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocumentEmailOrPhone("", "", "+5511")
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocumentEmailOrPhone_EmailAndPhone(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`email`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocumentEmailOrPhone("", "a@x.io", "+5511")
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocumentEmailOrPhone_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind`).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByDocumentEmailOrPhone("111", "", "")
	if err != nil || got != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByDocumentEmailOrPhone_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`cpf_blind`).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if _, err := repo.GetCustomerByDocumentEmailOrPhone("111", "", ""); err == nil {
		t.Fatal()
	}
}

func TestGetCustomerByDocumentEmailOrPhone_EncryptError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewCustomerRepository(db)
	if _, err := repo.GetCustomerByDocumentEmailOrPhone("111", "", ""); err == nil {
		t.Fatal()
	}
}

func TestGetCustomerByEmail_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`email = `).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByEmail("a@x.io")
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByEmail_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`email = `).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByEmail("a@x.io")
	if err != nil || got != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByEmail_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`email = `).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if _, err := repo.GetCustomerByEmail("a@x.io"); err == nil {
		t.Fatal()
	}
}

func TestGetCustomerByPhone_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`mobile_number = `).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByPhone("+55")
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByPhone_NotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`mobile_number = `).WillReturnError(gorm.ErrRecordNotFound)
	repo := NewCustomerRepository(db)
	got, err := repo.GetCustomerByPhone("+55")
	if err != nil || got != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetCustomerByPhone_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`mobile_number = `).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if _, err := repo.GetCustomerByPhone("+55"); err == nil {
		t.Fatal()
	}
}

func TestUpdateCustomer_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "customers"`).WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`INSERT INTO "customers"`).WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewCustomerRepository(db)
	if err := repo.UpdateCustomer(&customer.Customer{ID: "c1", Email: "a@x.io", CPF: "111"}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateCustomer_EncryptError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewCustomerRepository(db)
	if err := repo.UpdateCustomer(&customer.Customer{ID: "c1", CPF: "111"}); err == nil {
		t.Fatal()
	}
}

func TestUpdateCustomer_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "customers"`).WillReturnError(errors.New("db"))
	mock.ExpectExec(`INSERT INTO "customers"`).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if err := repo.UpdateCustomer(&customer.Customer{ID: "c1"}); err == nil {
		t.Fatal()
	}
}

func TestListCustomersByUser_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "customers" WHERE user_id`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1").AddRow("c2"))
	repo := NewCustomerRepository(db)
	got, err := repo.ListCustomersByUser("u1")
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %d", err, len(got))
	}
}

func TestListCustomersByUser_DBError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT .* FROM "customers" WHERE user_id`).WillReturnError(errors.New("db"))
	repo := NewCustomerRepository(db)
	if _, err := repo.ListCustomersByUser("u1"); err == nil {
		t.Fatal()
	}
}
