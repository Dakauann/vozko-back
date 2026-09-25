package whatsapp_campaign_entry

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func newEntryDB(t *testing.T) (*repository, sqlmock.Sqlmock, *sql.DB) {
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
	return &repository{db: db}, mock, sqlDB
}

func TestAReadReceiptStampsSendDeliveryAndReadInTheStatusUpdate(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "whatsapp_campaign_entries" SET ` +
		`"delivered_at"=COALESCE\(delivered_at, .+\),` +
		`"read_at"=COALESCE\(read_at, .+\),` +
		`"sent_at"=COALESCE\(sent_at, .+\),` +
		`"status"=.+ WHERE message_id = .+`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateStatusByMessageID("wamid.1", wce.SendStatusRead); err != nil {
		t.Fatalf("UpdateStatusByMessageID() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestADispatchFailureStampsFailedAtWithTheErrorInOneStatement(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "whatsapp_campaign_entries" SET ` +
		`"error_code"=.+,"error_message"=.+,` +
		`"failed_at"=COALESCE\(failed_at, .+\),` +
		`"status"=.+ WHERE id = .+`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateStatus("entry-1", wce.SendStatusFailed, "", 131026, "undeliverable"); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestASendByNumberStampsSentAt(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "whatsapp_campaign_entries" SET ` +
		`"message_id"=.+,"sent_at"=COALESCE\(sent_at, .+\),"status"=.+` +
		`WHERE campaign_id = .+ AND lead_id IN \(SELECT "id" FROM "leads" WHERE number = .+\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateStatusByNumber("campaign-1", "5511999999999", wce.SendStatusSent, "wamid.2"); err != nil {
		t.Fatalf("UpdateStatusByNumber() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAResetClearsEveryMilestoneWithTheStatus(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "whatsapp_campaign_entries" SET `+
		`"delivered_at"=.+,"failed_at"=.+,"message_id"=.+,"read_at"=.+,"sent_at"=.+,"status"=.+`+
		`WHERE campaign_id = .+`).
		WithArgs(nil, nil, "", nil, nil, "PENDING", sqlmock.AnyArg(), "campaign-1").
		WillReturnResult(sqlmock.NewResult(0, 3))

	if _, err := repo.ResetAllStatuses("campaign-1"); err != nil {
		t.Fatalf("ResetAllStatuses() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
