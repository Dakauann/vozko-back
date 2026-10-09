package calllist_repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/calls/calllist"
)

const (
	ws     = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"
	list   = "11111111-1111-4111-8111-111111111111"
	worker = "7d3e1f00-1111-4c2b-8f00-aa00bb00cc01"
	itemA  = "22222222-2222-4222-8222-222222222222"
)

var now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func anyArgs(n int) []driver.Value {
	args := make([]driver.Value, n)
	for i := range args {
		args[i] = sqlmock.AnyArg()
	}
	return args
}

func TestEveryStatementBindsOnlyItsOwnPlaceholdersAndNoJSONBQuestionOperator(t *testing.T) {
	statements := map[string]string{
		"updateList": updateListSQL, "claimBuild": claimBuildSQL, "lockBuild": lockBuildSQL, "appendItems": appendItemsSQL,
		"advanceBuild": advanceBuildSQL, "finishBuild": finishBuildSQL, "buildable": buildableSQL, "failExhausted": failExhaustedSQL,
		"items": itemsSQL + itemsAfterSQL + itemStateSQL + itemsOrderSQL, "releaseExpired": releaseExpiredSQL, "held": heldSQL, "dueCallback": dueCallbackSQL,
		"expired": expiredSQL, "queued": queuedSQL, "writeItem": writeItemSQL, "progress": progressSQL,
	}
	want := map[string]int{
		"updateList": 6, "claimBuild": 7, "lockBuild": 3, "appendItems": 9, "advanceBuild": 7, "finishBuild": 7, "buildable": 3,
		"failExhausted": 4, "items": 5, "releaseExpired": 4, "held": 2, "dueCallback": 3, "expired": 3, "queued": 2,
		"writeItem": 13, "progress": 6,
	}
	for name, sql := range statements {
		if got := strings.Count(sql, "?"); got != want[name] {
			t.Errorf("%s binds %d placeholders, want %d: %s", name, got, want[name], sql)
		}
		for _, op := range []string{"?|", "?&", "@?", " ? '"} {
			if strings.Contains(sql, op) {
				t.Errorf("%s uses %q, which GORM would bind as a placeholder", name, op)
			}
		}
	}
}

func TestAppendingABatchBindsOneArrayPerColumnWhateverItsSize(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	items := make([]calllist.Item, 0, 2000)
	for i := 0; i < 2000; i++ {
		items = append(items, calllist.Item{LeadID: "33333333-3333-4333-8333-333333333333", Phone: "5511987654321"})
	}
	mock.ExpectBegin()
	mock.ExpectQuery(numbered(lockBuildSQL)).WithArgs(ws, list, "claim-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "status", "item_count", "skipped", "phone_source"}).
			AddRow(list, ws, "building", 40, []byte(`{"gone":1}`), "identity"))
	mock.ExpectExec(numbered(appendItemsSQL)).WithArgs(anyArgs(9)...).WillReturnResult(sqlmock.NewResult(0, 1999))
	mock.ExpectExec(numbered(advanceBuildSQL)).
		WithArgs(int64(1999), `{"gone":3,"no_number":1}`, "44444444-4444-4444-8444-444444444444", now, now, ws, list).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := NewStore(db).AppendItems(context.Background(), calllist.BuildBatch{
		WorkspaceID: ws, ListID: list, Claim: "claim-1", Items: items,
		Skipped: calllist.Skips{calllist.SkipGone: 2, calllist.SkipNoNumber: 1}, Cursor: "44444444-4444-4444-8444-444444444444", At: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAppendingWithoutTheBuildClaimWritesNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(numbered(lockBuildSQL)).WithArgs(ws, list, "stale").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	err := NewStore(db).AppendItems(context.Background(), calllist.BuildBatch{WorkspaceID: ws, ListID: list, Claim: "stale", At: now})
	if !errors.Is(err, calllist.ErrBuildClaimLost) {
		t.Fatalf("AppendItems = %v, want ErrBuildClaimLost", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNextReleasesTheWorkersExpiredReservationThenClaimsInQueueOrder(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectExec(numbered(workerLockSQL)).WithArgs("call_list_worker:" + ws + ":" + worker).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(numbered(releaseExpiredSQL)).WithArgs(now, ws, worker, now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(numbered(heldSQL)).WithArgs(ws, worker).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(numbered(dueCallbackSQL)).WithArgs(ws, list, now).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(numbered(expiredSQL)).WithArgs(ws, list, now).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(numbered(queuedSQL)).WithArgs(ws, list).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "list_id", "lead_id", "phone", "position", "state"}).
			AddRow(itemA, ws, list, "33333333-3333-4333-8333-333333333333", "5511987654321", 7, "pending"))
	until := now.Add(calllist.ReservationTTL)
	mock.ExpectExec(numbered(writeItemSQL)).
		WithArgs("reserved", worker, until, nil, nil, nil, nil, nil, nil, nil, now, ws, itemA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	item, err := NewStore(db).Next(context.Background(), calllist.NextClaim{WorkspaceID: ws, ListID: list, UserID: worker, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.ID != itemA || item.ReservedBy != worker || item.Position != 7 {
		t.Fatalf("item = %+v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNextRefusesWhileTheWorkerHoldsAnItemOfAnotherList(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectBegin()
	mock.ExpectExec(numbered(workerLockSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(numbered(releaseExpiredSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(numbered(heldSQL)).WithArgs(ws, worker).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "list_id", "state", "reserved_by", "reserved_until"}).
			AddRow(itemA, ws, "99999999-9999-4999-8999-999999999999", "reserved", worker, now.Add(time.Minute)))
	mock.ExpectRollback()

	_, err := NewStore(db).Next(context.Background(), calllist.NextClaim{WorkspaceID: ws, ListID: list, UserID: worker, Now: now})
	if !errors.Is(err, calllist.ErrReservationHeld) {
		t.Fatalf("Next = %v, want ErrReservationHeld", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIdsThatAreNotUUIDsNeverReachTheDatabase(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	store := NewStore(db)
	ctx := context.Background()
	if _, err := store.Get(ctx, ws, "not-a-uuid"); !errors.Is(err, calllist.ErrListNotFound) {
		t.Fatalf("Get = %v", err)
	}
	if _, err := store.Item(ctx, ws, "nope"); !errors.Is(err, calllist.ErrItemNotFound) {
		t.Fatalf("Item = %v", err)
	}
	if _, err := store.Next(ctx, calllist.NextClaim{WorkspaceID: ws, ListID: list, UserID: "someone", Now: now}); !errors.Is(err, calllist.ErrActorRequired) {
		t.Fatalf("Next = %v", err)
	}
	if _, err := store.Mutate(ctx, "", itemA, nil); !errors.Is(err, calllist.ErrItemNotFound) {
		t.Fatalf("Mutate = %v", err)
	}
	page, err := store.Page(ctx, calllist.ListQuery{WorkspaceID: ws, ViewerID: "nobody"})
	if err != nil || len(page.Lists) != 0 {
		t.Fatalf("Page for a viewer without an id = %+v, %v", page, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAMemberSeesOnlyTheListsTheyAreAssignedTo(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	where := pageFilterSQL + assignedSQL + statusSQL
	mock.ExpectQuery(numbered("SELECT COUNT(*) FROM call_lists"+where)).WithArgs(ws, worker, "active").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(numbered("SELECT * FROM call_lists"+where+pageOrderSQL)).WithArgs(ws, worker, "active", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "assignee_ids", "status", "phone_source", "skipped"}).
			AddRow(list, ws, "Retorno", "{"+worker+"}", "active", "identity", []byte(`{}`)))

	page, err := NewStore(db).Page(context.Background(), calllist.ListQuery{WorkspaceID: ws, ViewerID: worker, Status: calllist.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Lists) != 1 || page.Lists[0].AssigneeIDs[0] != worker {
		t.Fatalf("page = %+v", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func numbered(query string) string {
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return regexp.QuoteMeta(b.String())
}

func TestItemPositionsFollowTheRowsActuallyWritten(t *testing.T) {
	for _, part := range []string{
		"COALESCE(MAX(e.position), 0) FROM call_list_items e WHERE e.list_id = ?::uuid",
		"row_number() OVER (ORDER BY f.ord)",
		"WITH ORDINALITY",
		"DISTINCT ON (b.lead_id)",
		"NOT EXISTS (SELECT 1 FROM call_list_items e WHERE e.list_id = ?::uuid AND e.lead_id = b.lead_id)",
	} {
		if !strings.Contains(appendItemsSQL, part) {
			t.Errorf("appendItemsSQL lacks %q: %s", part, appendItemsSQL)
		}
	}
}

func TestAttemptsCountOnlyCallsPlacedToTheLead(t *testing.T) {
	if !strings.Contains(itemsSQL, "ca.direction = 'outbound' AND ca.deleted_at IS NULL") {
		t.Fatalf("itemsSQL counts more than the outbound calls: %s", itemsSQL)
	}
}

func TestAnUpdateWritesTheNameTheMembersAndTheStatusOnly(t *testing.T) {
	if strings.Contains(updateListSQL, "signed_off") {
		t.Fatalf("a call list keeps no sign-off: %s", updateListSQL)
	}
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(numbered(updateListSQL)).
		WithArgs("Retorno", sqlmock.AnyArg(), "active", now, ws, list).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := NewStore(db).Update(context.Background(), &calllist.List{ID: list, WorkspaceID: ws, Name: "Retorno", AssigneeIDs: []string{worker},
		Status: calllist.StatusActive, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var itemViewColumns = []string{"id", "workspace_id", "list_id", "lead_id", "phone", "position", "state", "callback_at", "last_call_id",
	"lead_name", "lead_number", "lead_district", "lead_city", "call_id", "call_status", "call_direction", "call_agent_id", "call_lead_id", "call_deleted", "attempts"}

func TestTheOtherTabsKeepThePositionOrder(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(numbered(itemsSQL+itemsAfterSQL+itemStateSQL+itemsOrderSQL)).WithArgs(ws, list, 0, "closed", calllist.DefaultItemPage+1).
		WillReturnRows(sqlmock.NewRows(itemViewColumns))
	mock.ExpectQuery(numbered(itemsSQL+itemsAfterSQL+itemsOrderSQL)).WithArgs(ws, list, 4, calllist.DefaultItemPage+1).
		WillReturnRows(sqlmock.NewRows(itemViewColumns))
	store := NewStore(db)
	if _, err := store.Items(context.Background(), calllist.ItemQuery{WorkspaceID: ws, ListID: list, State: calllist.StateClosed}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Items(context.Background(), calllist.ItemQuery{WorkspaceID: ws, ListID: list, AfterPosition: 4}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAMutationMovesTheListProgressByWhatChangedOnTheItem(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	leadA := "33333333-3333-4333-8333-333333333333"
	callA := "55555555-5555-4555-8555-555555555555"
	until := now.Add(time.Minute)
	itemRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "workspace_id", "list_id", "lead_id", "phone", "position", "state", "reserved_by", "reserved_until"}).
			AddRow(itemA, ws, list, leadA, "5511987654321", 1, "reserved", worker, until)
	}
	listRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "workspace_id", "status", "phone_source", "skipped"}).AddRow(list, ws, "active", "identity", []byte(`{}`))
	}
	mock.ExpectBegin()
	mock.ExpectQuery(numbered(lockItemSQL)).WithArgs(ws, itemA).WillReturnRows(itemRow())
	mock.ExpectQuery(numbered(getListSQL)).WithArgs(ws, list).WillReturnRows(listRow())
	mock.ExpectExec(numbered(writeItemSQL)).WithArgs(anyArgs(13)...).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(numbered(progressSQL)).WithArgs(0, 1, 0, now, ws, list).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(numbered(lockItemSQL)).WithArgs(ws, itemA).WillReturnRows(itemRow())
	mock.ExpectQuery(numbered(getListSQL)).WithArgs(ws, list).WillReturnRows(listRow())
	mock.ExpectExec(numbered(writeItemSQL)).WithArgs(anyArgs(13)...).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	store := NewStore(db)
	stamped, err := store.Mutate(context.Background(), ws, itemA, func(i *calllist.Item, _ *calllist.List) error {
		return i.Stamp(worker, callA, now)
	})
	if err != nil || stamped.LastCallID != callA {
		t.Fatalf("stamp = %+v, %v", stamped, err)
	}
	if _, err := store.Mutate(context.Background(), ws, itemA, func(i *calllist.Item, _ *calllist.List) error {
		i.UpdatedAt = now
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
