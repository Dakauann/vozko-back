package analytics_repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInvoiceAccountsCountTemplateSendsNetOfRefundsPerWhatsAppAccount(t *testing.T) {
	db, mock, sqlDB := newMetaCostDB(t)
	defer sqlDB.Close()
	input := metaCostInput()

	mock.ExpectBegin()
	mock.ExpectExec(`SET LOCAL jit = off`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SET LOCAL statement_timeout`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`regexp_replace\(bt\.reference_id, '\^refund:', ''\)::uuid[\s\S]*bt\.reference_id ~\* [\s\S]*THEN 1[\s\S]*THEN -1[\s\S]*FROM whatsapp_business_accounts a[\s\S]*a\.deleted_at IS NULL`).
		WithArgs(input.StartDate, input.EndDate, input.StartDate, input.EndDate).
		WillReturnRows(sqlmock.NewRows([]string{"waba_id", "name", "provider", "access_token", "template_sends", "service_messages"}).
			AddRow("1029", "Clínica", "meta", "tok", 1_000, 40))
	mock.ExpectCommit()

	accounts, err := (&repository{db: db}).InvoiceAccounts(input.StartDate, input.EndDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].WABAID != "1029" || accounts[0].TemplateSends != 1_000 || accounts[0].ServiceMessages != 40 || accounts[0].AccessToken != "tok" {
		t.Fatalf("accounts %+v", accounts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
