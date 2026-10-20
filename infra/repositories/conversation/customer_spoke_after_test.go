package conversation_repository

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
)

func TestTheCustomerSpokeCheckIsOneExistenceQueryWithThreeBindings(t *testing.T) {
	if got := strings.Count(customerSpokeAfterSQL, "?"); got != 3 {
		t.Fatalf("placeholders = %d, want entry id, entry type and the run end", got)
	}
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()
	after := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT EXISTS \(\s*SELECT 1 FROM conversation_messages`).
		WithArgs("entry-1", "telegram", after).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	spoke, err := (&repository{db: db}).CustomerSpokeAfter("entry-1", shared.EntryTypeTelegram, after)
	if err != nil || !spoke {
		t.Fatalf("spoke = %v, err = %v", spoke, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnreadableCustomerSpokeCheckIsReported(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT EXISTS`).WillReturnError(errors.New("db down"))

	if _, err := (&repository{db: db}).CustomerSpokeAfter("entry-1", shared.EntryTypeTelegram, time.Now()); err == nil {
		t.Fatal("a failed read must not answer that the customer stayed quiet")
	}
}
