package order_repository

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/inventory"
	"vozko/domain/order"
	"vozko/domain/shared"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

var testPIISvc *pii.Service

func TestMain(m *testing.M) {
	enc := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("C", 32)))
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
		panic("PII init: " + err.Error())
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

type fakeStockService struct {
	snapshot inventory.VariantStockSnapshot
	err      error
}

func (f *fakeStockService) GetSnapshot(variantID string) (inventory.VariantStockSnapshot, error) {
	return f.snapshot, f.err
}

func (f *fakeStockService) GetSnapshots(variantIDs []string) (map[string]inventory.VariantStockSnapshot, error) {
	return nil, nil
}

func (f *fakeStockService) RecordLaunch(variantID string, quantity int, metadata inventory.VariantStockMetadata) (inventory.VariantStockSnapshot, error) {
	return inventory.VariantStockSnapshot{}, nil
}

func TestEncryptOrderDocument_Empty(t *testing.T) {
	enc, bi, err := encryptOrderDocument("")
	if err != nil || enc.Valid || bi != nil {
		t.Fatalf("%v %+v %v", err, enc, bi)
	}
}

func TestEncryptOrderDocument_NonEmpty(t *testing.T) {
	enc, bi, err := encryptOrderDocument("99988877766")
	if err != nil || !enc.Valid || len(bi) == 0 {
		t.Fatalf("%v %+v %v", err, enc, bi)
	}
}

func TestEncryptOrderDocument_Error(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	if _, _, err := encryptOrderDocument("111"); err == nil {
		t.Fatal()
	}
}

func TestCreate_EncryptError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewRepository(db, nil)
	if err := repo.Create(&order.Order{CustomerDocument: "111"}); err == nil {
		t.Fatal()
	}
}

func TestCreate_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_items"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_item_options"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	repo := NewRepository(db, nil)
	ord := &order.Order{
		ID:               "o1",
		CustomerDocument: "111",
		Items: []order.OrderItem{{
			ProductID: "p1", VariantID: "v1", SKU: "s", Quantity: 1, UnitPrice: 10, TotalPrice: 10,
			Options: []order.OrderItemOption{{OptionType: "size", OptionValue: "M"}},
		}},
	}
	if err := repo.Create(ord); err != nil {
		t.Fatal(err)
	}
}

func TestCreate_OrderInsertError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	repo := NewRepository(db, nil)
	if err := repo.Create(&order.Order{ID: "o1"}); err == nil {
		t.Fatal()
	}
}

func TestCreate_ItemInsertError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_items"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	repo := NewRepository(db, nil)
	ord := &order.Order{
		ID:    "o1",
		Items: []order.OrderItem{{ProductID: "p1"}},
	}
	if err := repo.Create(ord); err == nil {
		t.Fatal()
	}
}

func TestCreate_OptionInsertError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_items"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_item_options"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	repo := NewRepository(db, nil)
	ord := &order.Order{
		ID: "o1",
		Items: []order.OrderItem{{
			ProductID: "p1",
			Options:   []order.OrderItemOption{{OptionType: "size", OptionValue: "M"}},
		}},
	}
	if err := repo.Create(ord); err == nil {
		t.Fatal()
	}
}

func TestCreateWithInventoryReservation_EncryptError(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewRepository(db, &fakeStockService{})
	if err := repo.CreateWithInventoryReservation(&order.Order{CustomerDocument: "111"}, nil); err == nil {
		t.Fatal()
	}
}

func TestCreateWithInventoryReservation_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "variants" WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "inventory"}).AddRow("v1", 10))
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_items"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	stock := &fakeStockService{snapshot: inventory.VariantStockSnapshot{Launched: 0, Sold: 0, Reserved: 0}}
	repo := NewRepository(db, stock)
	ord := &order.Order{
		ID:    "o1",
		Items: []order.OrderItem{{ProductID: "p1", VariantID: "v1", Quantity: 1}},
	}
	updates := []order.InventoryUpdate{{VariantID: "v1", Quantity: 1}}
	if err := repo.CreateWithInventoryReservation(ord, updates); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithInventoryReservation_VariantNotFound(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "variants" WHERE id`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	repo := NewRepository(db, &fakeStockService{})
	updates := []order.InventoryUpdate{{VariantID: "v1", Quantity: 1}}
	if err := repo.CreateWithInventoryReservation(&order.Order{ID: "o1"}, updates); err == nil {
		t.Fatal()
	}
}

func TestCreateWithInventoryReservation_SnapshotError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "variants" WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "inventory"}).AddRow("v1", 10))
	mock.ExpectRollback()
	stock := &fakeStockService{err: errors.New("snap")}
	repo := NewRepository(db, stock)
	updates := []order.InventoryUpdate{{VariantID: "v1", Quantity: 1}}
	if err := repo.CreateWithInventoryReservation(&order.Order{ID: "o1"}, updates); err == nil {
		t.Fatal()
	}
}

func TestCreateWithInventoryReservation_InsufficientStock(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "variants" WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "inventory"}).AddRow("v1", 0))
	mock.ExpectRollback()
	stock := &fakeStockService{snapshot: inventory.VariantStockSnapshot{Sold: 10}}
	repo := NewRepository(db, stock)
	updates := []order.InventoryUpdate{{VariantID: "v1", Quantity: -1}}
	if err := repo.CreateWithInventoryReservation(&order.Order{ID: "o1"}, updates); !errors.Is(err, order.ErrInsufficientStock) {
		t.Fatalf("got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewRepository(db, nil)
	if err := repo.Update(&order.Order{ID: "o1", UserID: "u1", Status: order.StatusPaid}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateStatus(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewRepository(db, nil)
	if err := repo.UpdateStatus("o1", order.StatusPaid); err != nil {
		t.Fatal(err)
	}
}

func TestCancelOrder(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectExec(`UPDATE "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewRepository(db, nil)
	if err := repo.CancelOrder("o1"); err != nil {
		t.Fatal(err)
	}
}

func encryptedDoc(t *testing.T, plain string) []byte {
	t.Helper()
	v, err := piigorm.NewEncrypted(plain).Value()
	if err != nil {
		t.Fatal(err)
	}
	return v.([]byte)
}

func TestGetByID_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`FROM "orders" WHERE .*id =`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "customer_document"}).
			AddRow("o1", "u1", encryptedDoc(t, "111")))

	mock.ExpectQuery(`SELECT .* FROM "order_items"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "payments"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "tickets"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := NewRepository(db, nil)
	got, err := repo.GetByID("u1", "o1")
	if err != nil || got == nil || got.CustomerDocument != "111" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetByID_Error(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`FROM "orders" WHERE .*id =`).WillReturnError(errors.New("db"))
	repo := NewRepository(db, nil)
	if _, err := repo.GetByID("u1", "o1"); err == nil {
		t.Fatal()
	}
}

func TestGetByIDForSystem_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`FROM "orders" WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "customer_document"}).
			AddRow("o1", encryptedDoc(t, "222")))
	mock.ExpectQuery(`SELECT .* FROM "order_items"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "payments"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "tickets"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := NewRepository(db, nil)
	got, err := repo.GetByIDForSystem("o1")
	if err != nil || got == nil || got.CustomerDocument != "222" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetByIDForSystem_Error(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`FROM "orders" WHERE id`).WillReturnError(errors.New("db"))
	repo := NewRepository(db, nil)
	if _, err := repo.GetByIDForSystem("o1"); err == nil {
		t.Fatal()
	}
}

func TestGetExpiredOrders_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`FROM "orders" WHERE .*status =`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "customer_document"}).
			AddRow("o1", encryptedDoc(t, "333")))
	mock.ExpectQuery(`SELECT .* FROM "order_items"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "payments"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "tickets"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := NewRepository(db, nil)
	got, err := repo.GetExpiredOrders()
	if err != nil || len(got) != 1 || got[0].CustomerDocument != "333" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetExpiredOrders_Error(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`FROM "orders" WHERE .*status =`).WillReturnError(errors.New("db"))
	repo := NewRepository(db, nil)
	if _, err := repo.GetExpiredOrders(); err == nil {
		t.Fatal()
	}
}

func TestListByWorkspace_Success(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	mock.ExpectQuery(`SELECT DISTINCT ON`).WillReturnRows(sqlmock.NewRows([]string{"id", "customer_document"}).
		AddRow("o1", encryptedDoc(t, "111")))
	mock.ExpectQuery(`SELECT .* FROM "order_items"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "payments"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "tickets"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	repo := NewRepository(db, nil)
	page, err := repo.ListByWorkspace(order.ListOrdersInput{
		UserID: "u1",
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{Page: 1, PageSize: 10},
			Filters: []shared.Filter{
				{Field: "status", Operator: shared.FilterOpIn, Values: []string{"paid", "pending"}},
				{Field: "status", Values: []string{"paid"}},
				{Field: "search", Values: []string{"alice"}},
				{Field: "customerdocument", Values: []string{"111"}},
				{Field: "customername", Values: []string{"alice"}},
				{Field: "createdat", Operator: shared.FilterOpGte, Values: []string{"2024-01-01T00:00:00Z"}},
				{Field: "createdat", Operator: shared.FilterOpLte, Values: []string{"2024-12-31T00:00:00Z"}},
				{Field: "createdat", Values: []string{"2024-06-01T00:00:00Z"}},
				{Field: "createdat", Values: []string{"bad"}},
				{Field: "updatedat", Operator: shared.FilterOpGte, Values: []string{"2024-01-01T00:00:00Z"}},
				{Field: "updatedat", Operator: shared.FilterOpLte, Values: []string{"2024-12-31T00:00:00Z"}},
				{Field: "updatedat", Values: []string{"2024-06-01T00:00:00Z"}},
				{Field: "updatedat", Values: []string{"bad"}},
				{Field: "expiresat", Operator: shared.FilterOpGte, Values: []string{"2024-01-01T00:00:00Z"}},
				{Field: "expiresat", Operator: shared.FilterOpLte, Values: []string{"2024-12-31T00:00:00Z"}},
				{Field: "expiresat", Values: []string{"2024-06-01T00:00:00Z"}},
				{Field: "expiresat", Values: []string{"bad"}},
				{Field: "totalmin", Values: []string{"10"}},
				{Field: "totalmin", Values: []string{"bad"}},
				{Field: "totalmax", Values: []string{"100"}},
				{Field: "totalmax", Values: []string{"bad"}},
				{Field: "paymentstatus", Values: []string{"received"}},
				{Field: "addressid", Values: []string{"a1"}},
				{Field: "shippingaddressid", Values: []string{"a2"}},
				{Field: "ignored", Values: []string{"x"}},
				{Field: "skip", Values: []string{}},
				{Field: "skip", Values: []string{"  "}},
			},
			Sorts: []shared.Sort{
				{Field: "createdat", Direction: shared.SortAsc},
				{Field: "updatedat", Direction: shared.SortDesc},
				{Field: "expiresat", Direction: shared.SortAsc},
				{Field: "totalamount", Direction: shared.SortDesc},
				{Field: "total_amount", Direction: shared.SortAsc},
				{Field: "status", Direction: shared.SortAsc},
				{Field: "unknown", Direction: shared.SortAsc},
			},
		},
	})
	if err != nil || page == nil || len(page.Items) != 1 {
		t.Fatalf("%v %+v", err, page)
	}
}

func TestListByWorkspace_DefaultSortNoFilters(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectQuery(`SELECT DISTINCT ON`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := NewRepository(db, nil)
	page, err := repo.ListByWorkspace(order.ListOrdersInput{
		UserID:  "u1",
		Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 10}},
	})
	if err != nil || page == nil {
		t.Fatalf("%v %+v", err, page)
	}
}

func TestListByWorkspace_CountError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT COUNT`).WillReturnError(errors.New("db"))
	repo := NewRepository(db, nil)
	_, err := repo.ListByWorkspace(order.ListOrdersInput{
		UserID:  "u1",
		Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 10}},
	})
	if err == nil {
		t.Fatal()
	}
}

func TestListByWorkspace_DataError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectQuery(`SELECT DISTINCT ON`).WillReturnError(errors.New("db"))
	repo := NewRepository(db, nil)
	_, err := repo.ListByWorkspace(order.ListOrdersInput{
		UserID:  "u1",
		Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 10}},
	})
	if err == nil {
		t.Fatal()
	}
}

func TestListByWorkspace_SearchBlindIndexFallback(t *testing.T) {
	piigorm.SetService(nil)
	t.Cleanup(func() { piigorm.SetService(testPIISvc) })

	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectQuery(`SELECT DISTINCT ON`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	repo := NewRepository(db, nil)
	_, err := repo.ListByWorkspace(order.ListOrdersInput{
		UserID: "u1",
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{Page: 1, PageSize: 10},
			Filters: []shared.Filter{
				{Field: "search", Values: []string{"alice"}},
				{Field: "customerdocument", Values: []string{"111"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCancelExpiredOrdersAndReturnInventory_Empty(t *testing.T) {
	db, _, sdb := newMockDB(t)
	defer sdb.Close()
	repo := NewRepository(db, nil)
	if err := repo.CancelExpiredOrdersAndReturnInventory(nil); err != nil {
		t.Fatal(err)
	}
}

func TestCancelExpiredOrdersAndReturnInventory_FindError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "orders" WHERE id IN`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	repo := NewRepository(db, nil)
	if err := repo.CancelExpiredOrdersAndReturnInventory([]string{"o1"}); err == nil {
		t.Fatal()
	}
}

func TestSchemaOrderEncryptedField(t *testing.T) {
	enc, _, _ := encryptOrderDocument("X")
	o := schema.Order{CustomerDocument: enc}
	if o.CustomerDocument.String() != "X" {
		t.Fatal()
	}
}

func TestMapToOrder_FullPayload(t *testing.T) {
	r := &repository{}
	enc, _, _ := encryptOrderDocument("CPF1")
	last := "uX"
	tk := "TK"
	dbOrder := &schema.Order{
		ID:               "o1",
		UserID:           "u1",
		CustomerDocument: enc,
		Items: []schema.OrderItem{{
			ID: "i1", OrderID: "o1", ProductID: "p1", VariantID: "v1", SKU: "sku",
			Quantity: 2, UnitPrice: 5, TotalPrice: 10,
			Options: []schema.OrderItemOption{{OptionType: "size", OptionValue: "M"}},
		}},
		Payment: &schema.Payment{ID: "pay", Amount: 10, Status: "received", PixQrCode: "qr", PixCopy: "cp"},
		Ticket: &schema.Ticket{
			ID: "t1", OrderID: "o1", UserID: "u1", Status: "open", TrackingCode: &tk,
			LastStatusBy: &last,
			Documents: []schema.TicketDocument{{
				ID: "d1", TicketID: "t1", Type: "invoice", FileName: "f", ContentType: "pdf", URL: "u", UploadedBy: "u",
			}},
		},
	}
	got := r.mapToOrder(dbOrder)
	if got.CustomerDocument != "CPF1" || got.Payment == nil || got.Ticket == nil || len(got.Ticket.Documents) != 1 {
		t.Fatalf("%+v", got)
	}
	if len(got.Items) != 1 || len(got.Items[0].Options) != 1 {
		t.Fatal()
	}
}

func TestMapTicketToDomain_Nil(t *testing.T) {
	if mapTicketToDomain(nil) != nil {
		t.Fatal()
	}
}

func TestCreateWithInventoryReservation_OrderInsertError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "variants" WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "inventory"}).AddRow("v1", 10))
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	stock := &fakeStockService{}
	repo := NewRepository(db, stock)
	updates := []order.InventoryUpdate{{VariantID: "v1", Quantity: 1}}
	if err := repo.CreateWithInventoryReservation(&order.Order{ID: "o1"}, updates); err == nil {
		t.Fatal()
	}
}

func TestCreateWithInventoryReservation_ItemInsertError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "variants" WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "inventory"}).AddRow("v1", 10))
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_items"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	stock := &fakeStockService{}
	repo := NewRepository(db, stock)
	ord := &order.Order{ID: "o1", Items: []order.OrderItem{{ProductID: "p1"}}}
	updates := []order.InventoryUpdate{{VariantID: "v1", Quantity: 1}}
	if err := repo.CreateWithInventoryReservation(ord, updates); err == nil {
		t.Fatal()
	}
}

func TestCreateWithInventoryReservation_OptionInsertError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "variants" WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "inventory"}).AddRow("v1", 10))
	mock.ExpectExec(`INSERT INTO "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_items"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "order_item_options"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	stock := &fakeStockService{}
	repo := NewRepository(db, stock)
	ord := &order.Order{
		ID: "o1",
		Items: []order.OrderItem{{
			ProductID: "p1",
			Options:   []order.OrderItemOption{{OptionType: "x", OptionValue: "y"}},
		}},
	}
	updates := []order.InventoryUpdate{{VariantID: "v1", Quantity: 1}}
	if err := repo.CreateWithInventoryReservation(ord, updates); err == nil {
		t.Fatal()
	}
}

func TestCancelExpiredOrdersAndReturnInventory_AllBranches(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()

	mock.ExpectQuery(`FROM "orders" WHERE id IN`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("o1").AddRow("o2").AddRow("o3"))
	mock.ExpectQuery(`FROM "order_items" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`FROM "payments" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id", "order_id", "status"}).
		AddRow("p1", "o1", "PENDING").
		AddRow("p2", "o2", "RECEIVED"))
	mock.ExpectExec(`UPDATE "orders"`).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE "payments"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	repo := NewRepository(db, nil)
	if err := repo.CancelExpiredOrdersAndReturnInventory([]string{"o1", "o2", "o3"}); err != nil {
		t.Fatal(err)
	}
}

func TestCancelExpiredOrdersAndReturnInventory_OrderUpdateError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM "orders" WHERE id IN`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("o1"))
	mock.ExpectQuery(`FROM "order_items" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`FROM "payments" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`UPDATE "orders"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	repo := NewRepository(db, nil)
	if err := repo.CancelExpiredOrdersAndReturnInventory([]string{"o1"}); err == nil {
		t.Fatal()
	}
}

func TestCancelExpiredOrdersAndReturnInventory_PaymentUpdateError(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM "orders" WHERE id IN`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("o1"))
	mock.ExpectQuery(`FROM "order_items" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`FROM "payments" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id", "order_id", "status"}).
		AddRow("p1", "o1", "PENDING"))
	mock.ExpectExec(`UPDATE "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "payments"`).WillReturnError(errors.New("db"))
	mock.ExpectRollback()
	repo := NewRepository(db, nil)
	if err := repo.CancelExpiredOrdersAndReturnInventory([]string{"o1"}); err == nil {
		t.Fatal()
	}
}

func TestCancelExpiredOrdersAndReturnInventory_SkipReceivedInCash(t *testing.T) {
	db, mock, sdb := newMockDB(t)
	defer sdb.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM "orders" WHERE id IN`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("o1"))
	mock.ExpectQuery(`FROM "order_items" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`FROM "payments" WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id", "order_id", "status"}).
		AddRow("p1", "o1", "RECEIVED_IN_CASH"))
	mock.ExpectCommit()
	repo := NewRepository(db, nil)
	if err := repo.CancelExpiredOrdersAndReturnInventory([]string{"o1"}); err != nil {
		t.Fatal(err)
	}
}
