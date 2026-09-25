package unofficial_whatsapp_campaign_repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/campaign"
)

func newEntryDB(t *testing.T) (*entryRepository, sqlmock.Sqlmock, *sql.DB) {
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
	return &entryRepository{db: db}, mock, sqlDB
}

func TestAProviderReadReceiptStampsDeliveryAndRead(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "unofficial_whatsapp_campaign_entries" SET ` +
		`"delivered_at"=COALESCE\(delivered_at, .+\),` +
		`"read_at"=COALESCE\(read_at, .+\),` +
		`"sent_at"=COALESCE\(sent_at, .+\),` +
		`"status"=.+ WHERE \(provider_message_id = .+ AND status IN .+\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateStatusByProviderMessageID("provider-1", campaign.SendStatusRead); err != nil {
		t.Fatalf("UpdateStatusByProviderMessageID() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnofficialFailureStampsFailedAt(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "unofficial_whatsapp_campaign_entries" SET ` +
		`"error_code"=.+,"error_message"=.+,"failed_at"=COALESCE\(failed_at, .+\),"status"=.+ WHERE id = .+`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateStatus("entry-1", campaign.SendStatusFailed, "", 500, "provider down"); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnofficialResetClearsEveryMilestone(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "unofficial_whatsapp_campaign_entries" SET `+
		`"delivered_at"=.+,"error_code"=.+,"error_message"=.+,"failed_at"=.+,`+
		`"message_id"=.+,"provider_message_id"=.+,"read_at"=.+,"sent_at"=.+,"status"=.+ WHERE campaign_id = .+`).
		WithArgs(nil, 0, "", nil, nil, "", nil, nil, "PENDING", sqlmock.AnyArg(), "campaign-1").
		WillReturnResult(sqlmock.NewResult(0, 2))

	if _, err := repo.ResetAllStatuses("campaign-1"); err != nil {
		t.Fatalf("ResetAllStatuses() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
