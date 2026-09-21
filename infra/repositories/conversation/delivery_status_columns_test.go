package conversation_repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/conversation"
)

func newStatusDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
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

func TestUpdateDeliveryStatusMatchesEitherIDColumn(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	const providerID = "3EB027B8F1853217E3B8BB"

	mock.ExpectExec(`UPDATE .*conversation_messages.*whatsapp_message_id = .* OR .*external_message_id = `).
		WithArgs(
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			providerID,
			providerID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).UpdateDeliveryStatus(providerID, conversation.DeliveryStatusRead); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the update must reach external_message_id too: %v", err)
	}
}

func TestUpdateDeliveryStatusStillMatchesTheWhatsAppColumn(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	const wamid = "wamid.HBgMNTU1MTg5MTkwOTQ5FQIAEhgU"

	mock.ExpectExec(`whatsapp_message_id = `).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), wamid, wamid).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).UpdateDeliveryStatus(wamid, conversation.DeliveryStatusDelivered); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("official WhatsApp must keep working: %v", err)
	}
}

func TestUpdateDeliveryStatusWithReasonUsesTheSamePredicate(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	const providerID = "3EB0782E7A6648BE50D64C"

	mock.ExpectExec(`whatsapp_message_id = .* OR .*external_message_id = `).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).UpdateDeliveryStatusWithReason(
		providerID, conversation.DeliveryStatusFailed, 131026, "recipient not on WhatsApp"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the reason path must use the widened predicate: %v", err)
	}
}
