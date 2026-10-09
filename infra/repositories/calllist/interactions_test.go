package calllist_repository

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
)

func TestTheLatestInteractionIsTheNewestRealMessageOnAnyChannelOfTheLead(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if strings.Count(latestInteractionSQL, "?") != 1 {
		t.Fatalf("the lookup must bind only the lead: %s", latestInteractionSQL)
	}
	for _, part := range []string{"lead_entries.lead_id = ?", "ORDER BY m.created_at DESC LIMIT 1", "m.deleted_at IS NULL", "unofficial_whatsapp_conversations"} {
		if !strings.Contains(latestInteractionSQL, part) {
			t.Fatalf("lookup misses %q: %s", part, latestInteractionSQL)
		}
	}
	mock.ExpectQuery(numbered(latestInteractionSQL)).WithArgs("33333333-3333-4333-8333-333333333333").
		WillReturnRows(sqlmock.NewRows([]string{"entry_id", "entry_type", "at"}).AddRow(itemA, "telegram", now))

	got, err := NewInteractions(db).Latest(context.Background(), ws, "33333333-3333-4333-8333-333333333333")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.EntryID != itemA || got.EntryType != shared.EntryTypeTelegram || !got.At.Equal(now) {
		t.Fatalf("latest = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestALeadWithoutConversationsHasNoLatestInteraction(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(numbered(latestInteractionSQL)).WillReturnRows(sqlmock.NewRows([]string{"entry_id", "entry_type", "at"}))
	got, err := NewInteractions(db).Latest(context.Background(), ws, "33333333-3333-4333-8333-333333333333")
	if err != nil || got != nil {
		t.Fatalf("latest = %+v, %v", got, err)
	}
	if got, err := NewInteractions(db).Latest(context.Background(), ws, "nope"); err != nil || got != nil {
		t.Fatalf("a malformed lead = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
