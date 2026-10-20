package whatsapp_campaign_entry

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestFinishingAnOfficialConversationEndsTheStoredWait(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "whatsapp_campaign_entries" SET "awaiting_reply_since"=\$1,"close_outcome"=`).
		WithArgs(nil, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "finished", sqlmock.AnyArg(), "entry-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateConversationStatus("entry-1", wce.ConversationStatusWrite{Status: "finished", SetCloseMeta: true, CloseSource: "human", CloseReason: "manual"}); err != nil {
		t.Fatalf("UpdateConversationStatus() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReopeningAnOfficialConversationKeepsTheStoredWait(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "whatsapp_campaign_entries" SET "close_outcome"=\$1,"close_reason"=\$2,"close_source"=\$3,"closed_at"=\$4,"conversation_status"=\$5,"updated_at"=\$6 WHERE`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateConversationStatus("entry-1", wce.ConversationStatusWrite{Status: "new", ClearCloseMeta: true}); err != nil {
		t.Fatalf("UpdateConversationStatus() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
