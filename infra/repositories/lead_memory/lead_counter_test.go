package lead_memory_repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTheMemoryCountOfALeadIsOneCountInItsWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`^SELECT count\(\*\) FROM "lead_memories" WHERE \(workspace_id = \$1 AND lead_id = \$2\) AND "lead_memories"\."deleted_at" IS NULL$`).
		WithArgs("ws-1", "lead-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	got, err := NewLeadCounter(db).CountMemoriesOfLead(context.Background(), "ws-1", "lead-1")
	if err != nil || got != 7 {
		t.Fatalf("count = %d, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheMemoryCountRefusesWithoutAWorkspaceOrALead(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	counter := NewLeadCounter(db)
	for name, ids := range map[string][2]string{"no workspace": {" ", "lead-1"}, "no lead": {"ws-1", ""}} {
		if _, err := counter.CountMemoriesOfLead(context.Background(), ids[0], ids[1]); !errors.Is(err, ErrMemoryCountInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheMemoryCountStopsWithItsRequest(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewLeadCounter(db).CountMemoriesOfLead(ctx, "ws-1", "lead-1"); err == nil {
		t.Fatal("a cancelled request must not count")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
