package conversation_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestMarkOutboundStatusUpToOnlyUpgradesOlderOutboundRows(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()
	upTo := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE .*conversation_messages.* SET .*delivery_status.* WHERE \(entry_id = .* AND entry_type = .* AND direction = .* AND created_at <= .* AND delivery_status IN .* AND .*deleted_at.* IS NULL`).
		WithArgs(string(conversation.DeliveryStatusRead), sqlmock.AnyArg(), "entry-1", "facebook", "OUTBOUND", upTo, "", "sent", "delivered").
		WillReturnResult(sqlmock.NewResult(0, 4))

	n, err := NewRepository(db).(conversation.DeliveryWatermarkRepository).MarkOutboundStatusUpTo("entry-1", shared.EntryType("facebook"), conversation.DeliveryStatusRead, upTo)
	if err != nil || n != 4 {
		t.Fatalf("updated %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarkOutboundStatusUpToIgnoresNonProgressStatuses(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()
	n, err := NewRepository(db).(conversation.DeliveryWatermarkRepository).MarkOutboundStatusUpTo("entry-1", shared.EntryType("facebook"), conversation.DeliveryStatusFailed, time.Now())
	if err != nil || n != 0 {
		t.Fatalf("updated %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
