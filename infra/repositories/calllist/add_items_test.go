package calllist_repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/calls/calllist"
)

const leadA = "33333333-3333-4333-8333-333333333333"

func TestTheAdditionStatementsBindOnlyTheirOwnPlaceholders(t *testing.T) {
	cases := map[string]struct {
		sql  string
		want int
	}{
		"lockOpenList": {lockOpenListSQL, 2},
		"countAdded":   {countAddedSQL, 4},
	}
	for name, tc := range cases {
		if got := strings.Count(tc.sql, "?"); got != tc.want {
			t.Errorf("%s binds %d placeholders, want %d: %s", name, got, tc.want, tc.sql)
		}
	}
}

func TestAddingALeadLocksTheListInsertsThroughTheDedupingAppendAndCountsWhatWasWritten(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(numbered(lockOpenListSQL)).WithArgs(ws, list).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "status", "item_count", "phone_source"}).
			AddRow(list, ws, "active", 40, "identity"))
	mock.ExpectExec(numbered(appendItemsSQL)).WithArgs(anyArgs(9)...).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(numbered(countAddedSQL)).WithArgs(int64(1), now, ws, list).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	added, err := NewStore(db).AddItems(context.Background(), calllist.Addition{
		WorkspaceID: ws, ListID: list, At: now, Items: []calllist.Item{{LeadID: leadA, Phone: "5511987654321"}},
	})
	if err != nil || added != 1 {
		t.Fatalf("AddItems = %d, %v", added, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAddingALeadAlreadyOnTheListLeavesTheCountAlone(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(numbered(lockOpenListSQL)).WithArgs(ws, list).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "status", "item_count", "phone_source"}).
			AddRow(list, ws, "paused", 40, "identity"))
	mock.ExpectExec(numbered(appendItemsSQL)).WithArgs(anyArgs(9)...).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	added, err := NewStore(db).AddItems(context.Background(), calllist.Addition{
		WorkspaceID: ws, ListID: list, At: now, Items: []calllist.Item{{LeadID: leadA, Phone: "5511987654321"}},
	})
	if err != nil || added != 0 {
		t.Fatalf("AddItems = %d, %v", added, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAddingToAListThatTakesNoNewLeadsWritesNothing(t *testing.T) {
	cases := map[string]struct {
		status string
		want   error
	}{
		"building": {"building", calllist.ErrListBuilding},
		"archived": {"archived", calllist.ErrListNotActive},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(numbered(lockOpenListSQL)).WithArgs(ws, list).
				WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "status", "item_count", "phone_source"}).
					AddRow(list, ws, tc.status, 4, "identity"))
			mock.ExpectRollback()

			_, err := NewStore(db).AddItems(context.Background(), calllist.Addition{
				WorkspaceID: ws, ListID: list, At: now, Items: []calllist.Item{{LeadID: leadA, Phone: "5511987654321"}},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("AddItems = %v, want %v", err, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAnAdditionWithIdsThatAreNotUUIDsNeverReachesTheDatabase(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	store := NewStore(db)
	if _, err := store.AddItems(context.Background(), calllist.Addition{WorkspaceID: ws, ListID: "nope", At: now,
		Items: []calllist.Item{{LeadID: leadA, Phone: "1"}}}); !errors.Is(err, calllist.ErrListNotFound) {
		t.Fatalf("a bad list id = %v", err)
	}
	if _, err := store.AddItems(context.Background(), calllist.Addition{WorkspaceID: ws, ListID: list, At: now,
		Items: []calllist.Item{{LeadID: "not-a-lead", Phone: "1"}}}); !errors.Is(err, calllist.ErrLeadNotAdmitted) {
		t.Fatalf("a bad lead id = %v", err)
	}
	if _, err := store.AddItems(context.Background(), calllist.Addition{WorkspaceID: ws, ListID: list, At: now}); !errors.Is(err, calllist.ErrLeadNotAdmitted) {
		t.Fatalf("an empty addition = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
