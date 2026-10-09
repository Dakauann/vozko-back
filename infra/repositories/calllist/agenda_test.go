package calllist_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/calls/calllist"
)

func agendaBranch(sql string) string {
	return "(" + sql + agendaBranchOrderSQL + ")"
}

func agendaPage(branches ...string) string {
	return fmt.Sprintf(agendaPageSQL, strings.Join(branches, " UNION ALL "))
}

func TestTheAgendaReadsEachSegmentOnTheIndexItIsBuiltOn(t *testing.T) {
	key := "COALESCE(callback_at, 'infinity'::timestamptz)"
	pins := []struct {
		name, sql, part string
	}{
		{"branch order", agendaBranchOrderSQL, " ORDER BY " + key + ", position LIMIT ?"},
		{"due", agendaDueSQL, "SELECT id, 0 AS segment, " + key + " AS agenda_at, position FROM call_list_items"},
		{"due", agendaDueSQL, " AND " + key + " <= ?::timestamptz"},
		{"queue", agendaQueueSQL, "SELECT id, 1 AS segment"},
		{"queue", agendaQueueSQL, " AND " + key + " = 'infinity'::timestamptz"},
		{"waiting", agendaWaitingSQL, "SELECT id, 2 AS segment"},
		{"waiting", agendaWaitingSQL, " AND " + key + " > ?::timestamptz AND " + key + " < 'infinity'::timestamptz"},
		{"time cursor", agendaTimeAfterSQL, " AND (" + key + ", position) > (?::timestamptz, ?)"},
		{"position cursor", agendaPositionAfterSQL, " AND position > ?"},
		{"page", agendaPageSQL, " FROM (SELECT id, segment, agenda_at, position FROM (%s) segments ORDER BY segment, agenda_at, position LIMIT ?) agenda" +
			" JOIN call_list_items i ON i.id = agenda.id"},
		{"page", agendaPageSQL, "LEFT JOIN lead_addresses a ON a.lead_id = i.lead_id AND a.workspace_id = i.workspace_id AND a.is_primary"},
	}
	for _, pin := range pins {
		if !strings.Contains(pin.sql, pin.part) {
			t.Errorf("%s lacks %q: %s", pin.name, pin.part, pin.sql)
		}
	}
	if !strings.HasSuffix(agendaPageSQL, " ORDER BY agenda.segment, agenda.agenda_at, agenda.position") {
		t.Errorf("the page must keep the agenda order after its joins: %s", agendaPageSQL)
	}
	if cut, joined := strings.Index(agendaPageSQL, "LIMIT ?"), strings.Index(agendaPageSQL, " LEFT JOIN "); cut < 0 || joined < cut {
		t.Errorf("the page must cut the agenda to its size before joining leads, addresses and calls: %s", agendaPageSQL)
	}
	for _, sql := range []string{agendaDueSQL, agendaQueueSQL, agendaWaitingSQL} {
		if !strings.Contains(sql, " WHERE workspace_id = ? AND list_id = ? AND state = 'pending'") {
			t.Errorf("a segment reads outside the pending items of its list: %s", sql)
		}
	}
	if strings.Contains(agendaTimeAfterSQL, "SELECT") || strings.Contains(agendaPositionAfterSQL, "SELECT") {
		t.Errorf("the cursor must be bound values, never a lookup of a row that can change: %s", agendaTimeAfterSQL)
	}
}

func TestEachAgendaStatementBindsOnlyItsOwnPlaceholders(t *testing.T) {
	cases := map[string]struct {
		sql  string
		want int
	}{
		"due":             {agendaDueSQL, 3},
		"queue":           {agendaQueueSQL, 2},
		"waiting":         {agendaWaitingSQL, 3},
		"time cursor":     {agendaTimeAfterSQL, 2},
		"position cursor": {agendaPositionAfterSQL, 1},
		"branch order":    {agendaBranchOrderSQL, 1},
		"page":            {agendaPageSQL, 1},
	}
	for name, tc := range cases {
		if got := strings.Count(tc.sql, "?"); got != tc.want {
			t.Errorf("%s binds %d placeholders, want %d: %s", name, got, tc.want, tc.sql)
		}
		for _, op := range []string{"?|", "?&", "@?", " ? '"} {
			if strings.Contains(tc.sql, op) {
				t.Errorf("%s uses %q, which GORM would bind as a placeholder", name, op)
			}
		}
	}
}

func TestThePendingTabReadsDueCallbacksThenTheQueueThenWaitingCallbacks(t *testing.T) {
	asOf := now
	due, ahead := now.Add(-time.Hour), now.Add(time.Hour)
	limit := calllist.DefaultItemPage + 1
	cases := []struct {
		name  string
		query calllist.ItemQuery
		sql   string
		args  []driver.Value
	}{
		{"the first page", calllist.ItemQuery{AsOf: asOf},
			agendaPage(agendaBranch(agendaDueSQL), agendaBranch(agendaQueueSQL), agendaBranch(agendaWaitingSQL)),
			[]driver.Value{ws, list, asOf, limit, ws, list, limit, ws, list, asOf, limit, limit}},
		{"after a due callback", calllist.ItemQuery{AsOf: asOf, AfterPosition: 7, AfterAt: &due},
			agendaPage(agendaBranch(agendaDueSQL+agendaTimeAfterSQL), agendaBranch(agendaQueueSQL), agendaBranch(agendaWaitingSQL)),
			[]driver.Value{ws, list, asOf, due, 7, limit, ws, list, limit, ws, list, asOf, limit, limit}},
		{"after a queued item", calllist.ItemQuery{AsOf: asOf, AfterPosition: 7},
			agendaPage(agendaBranch(agendaQueueSQL+agendaPositionAfterSQL), agendaBranch(agendaWaitingSQL)),
			[]driver.Value{ws, list, 7, limit, ws, list, asOf, limit, limit}},
		{"after a waiting callback", calllist.ItemQuery{AsOf: asOf, AfterPosition: 7, AfterAt: &ahead},
			agendaPage(agendaBranch(agendaWaitingSQL + agendaTimeAfterSQL)),
			[]driver.Value{ws, list, asOf, ahead, 7, limit, limit}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, sqlDB := newMockDB(t)
			defer sqlDB.Close()
			mock.ExpectQuery(numbered(tc.sql)).WithArgs(tc.args...).WillReturnRows(sqlmock.NewRows(itemViewColumns))
			q := tc.query
			q.WorkspaceID, q.ListID, q.State = ws, list, calllist.StatePending
			page, err := NewStore(db).Items(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if page.AsOf == nil || !page.AsOf.Equal(asOf) || page.Next != 0 || page.NextAt != nil {
				t.Fatalf("page = %+v", page)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAPendingPageWithoutItsInstantNeverReachesTheDatabase(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	_, err := NewStore(db).Items(context.Background(), calllist.ItemQuery{WorkspaceID: ws, ListID: list, State: calllist.StatePending, AfterPosition: 7})
	if !errors.Is(err, calllist.ErrItemCursorInvalid) {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestACutPendingPageCarriesTheKeyOfItsLastRow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	leadA, callA := "33333333-3333-4333-8333-333333333333", "55555555-5555-4555-8555-555555555555"
	due := now.Add(-time.Minute)
	asOf := now.Add(123 * time.Nanosecond)
	row := func(position int, callback driver.Value, deleted bool) []driver.Value {
		return []driver.Value{itemA, ws, list, leadA, "5511987654321", position, "pending", callback, callA, "Maria", "5511987654321", "Aldeia", "Barueri",
			callA, "completed", "outbound", worker, leadA, deleted, 1}
	}
	sql := agendaPage(agendaBranch(agendaDueSQL), agendaBranch(agendaQueueSQL), agendaBranch(agendaWaitingSQL))
	mock.ExpectQuery(numbered(sql)).WithArgs(ws, list, now, 2, ws, list, 2, ws, list, now, 2, 2).
		WillReturnRows(sqlmock.NewRows(itemViewColumns).AddRow(row(9, due, false)...).AddRow(row(3, nil, true)...))
	mock.ExpectQuery(numbered(sql)).WithArgs(ws, list, now, 2, ws, list, 2, ws, list, now, 2, 2).
		WillReturnRows(sqlmock.NewRows(itemViewColumns).AddRow(row(3, nil, true)...).AddRow(row(9, due, false)...))

	store := NewStore(db)
	page, err := store.Items(context.Background(), calllist.ItemQuery{WorkspaceID: ws, ListID: list, State: calllist.StatePending, AsOf: asOf, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 9 || page.NextAt == nil || !page.NextAt.Equal(due) || page.AsOf == nil || !page.AsOf.Equal(now) {
		t.Fatalf("cursor = next %d at %v as of %v, want 9 at %v as of %v", page.Next, page.NextAt, page.AsOf, due, now)
	}
	view := page.Items[0]
	if view.LeadDistrict != "Aldeia" || view.LeadCity != "Barueri" {
		t.Fatalf("place = %q, %q", view.LeadDistrict, view.LeadCity)
	}
	want := calllist.CallFacts{ID: callA, WorkspaceID: ws, LeadID: leadA, AgentID: worker}
	if view.StampedCall == nil || *view.StampedCall != want {
		t.Fatalf("stamped call = %+v, want %+v", view.StampedCall, want)
	}

	queued, err := store.Items(context.Background(), calllist.ItemQuery{WorkspaceID: ws, ListID: list, State: calllist.StatePending, AsOf: asOf, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Next != 3 || queued.NextAt != nil {
		t.Fatalf("a queued cursor = next %d at %v", queued.Next, queued.NextAt)
	}
	if gone := queued.Items[0]; gone.LastCall == nil || gone.StampedCall != nil {
		t.Fatalf("a deleted call keeps its outcome but is no call to close with: last %+v stamped %+v", gone.LastCall, gone.StampedCall)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
