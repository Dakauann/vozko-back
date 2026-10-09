package calllist_repository

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTheRecountReadsListsByIdAndCountsEachListUnderItsLock(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	other := "33333333-3333-4333-8333-333333333333"
	mock.ExpectQuery(numbered(recountPageSQL)).WithArgs(firstListID, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(list).AddRow(other))
	for _, id := range []string{list, other} {
		mock.ExpectBegin()
		mock.ExpectExec(numbered(lockListSQL)).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
		affected := int64(0)
		if id == list {
			affected = 1
		}
		mock.ExpectExec(numbered(recountSQL)).WithArgs(id, id).WillReturnResult(sqlmock.NewResult(0, affected))
		mock.ExpectCommit()
	}

	next, recounted, err := NewStore(db).RecountProgress(context.Background(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if next != other || recounted != 1 {
		t.Fatalf("next %q recounted %d, want %q and 1", next, recounted, other)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheRecountEndsWhenNoListIsLeft(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(numbered(recountPageSQL)).WithArgs(list, 50).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	next, recounted, err := NewStore(db).RecountProgress(context.Background(), list, 50)
	if err != nil || next != "" || recounted != 0 {
		t.Fatalf("next %q recounted %d err %v", next, recounted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestARecountCursorThatIsNotAListIdNeverReachesTheDatabase(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	for _, bad := range []struct {
		after string
		limit int
	}{{"list-1", 10}, {list, 0}} {
		if _, _, err := NewStore(db).RecountProgress(context.Background(), bad.after, bad.limit); err == nil {
			t.Fatalf("RecountProgress(%q, %d) passed", bad.after, bad.limit)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheRecountCountsWhatTheProgressMovesCountAndTheCountersNeverGoBelowZero(t *testing.T) {
	for name, tc := range map[string]struct {
		sql  string
		want int
	}{"page": {recountPageSQL, 2}, "lock": {lockListSQL, 1}, "recount": {recountSQL, 2}} {
		if got := strings.Count(tc.sql, "?"); got != tc.want {
			t.Errorf("%s binds %d placeholders, want %d: %s", name, got, tc.want, tc.sql)
		}
	}
	for _, part := range []string{
		"count(*) FILTER (WHERE last_call_id IS NOT NULL)",
		"count(*) FILTER (WHERE state <> 'closed' AND disposition = '_callback')",
		"IS DISTINCT FROM",
	} {
		if !strings.Contains(recountSQL, part) {
			t.Errorf("recountSQL lacks %q: %s", part, recountSQL)
		}
	}
	for _, part := range []string{"called_count = GREATEST(0, called_count + ?)", "callback_count = GREATEST(0, callback_count + ?)"} {
		if !strings.Contains(progressSQL, part) {
			t.Errorf("progressSQL lacks %q: %s", part, progressSQL)
		}
	}
}
