package conversation_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
)

func TestUnattributedServiceMessagesKeepTheFirstSightingAndTheLatestStatus(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)

	mock.ExpectExec(`INSERT INTO whatsapp_unattributed_service_messages .* ON CONFLICT \(whatsapp_message_id\) DO UPDATE SET status = EXCLUDED\.status, .*last_seen_at = EXCLUDED\.last_seen_at`).
		WithArgs("wamid.9", "885813321280568", "delivered", "service", "regular", at, at).
		WillReturnResult(sqlmock.NewResult(0, 1))

	m := conversation.UnattributedServiceMessage{WhatsAppMessageID: "wamid.9", PhoneNumberID: "885813321280568", Status: conversation.DeliveryStatusDelivered, Category: "service", PricingType: "regular", SeenAt: at}
	if err := NewUnattributedServiceMessageRepository(db).Record(m); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnattributedRecordNeedsTheMessageID(t *testing.T) {
	db, _, sqlDB := newStatusDB(t)
	defer sqlDB.Close()
	if err := NewUnattributedServiceMessageRepository(db).Record(conversation.UnattributedServiceMessage{}); err == nil {
		t.Fatal("a record without the provider message id cannot be counted or deduplicated")
	}
}
