package conversation_repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
)

func TestMarkAsReadTouchesOnlyWhatTheContactSent(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "conversation_messages" SET .* WHERE .*sender_kind = 'contact'`).
		WillReturnResult(sqlmock.NewResult(0, 2))

	n, err := NewRepository(db).MarkAsRead(conversation.MarkAsReadInput{EntryID: "entry-1", EntryType: "whatsapp", ReadBy: "user-1"})
	if err != nil || n != 2 {
		t.Fatalf("MarkAsRead() = %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUnreadCountsOnlyWhatTheContactSent(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT count\(\*\) FROM "conversation_messages" WHERE .*sender_kind = 'contact'`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	n, err := NewRepository(db).CountUnreadByEntry("entry-1", "whatsapp")
	if err != nil || n != 3 {
		t.Fatalf("CountUnreadByEntry() = %d, %v", n, err)
	}
}
