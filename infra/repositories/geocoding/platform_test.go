package geocoding_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/geocoding"
)

func TestTakeSlotKeepsTheMonthInTheHistoryInTheSameStatement(t *testing.T) {
	for _, want := range []string{
		"WITH archived AS (INSERT INTO geocoding_usage_months AS m (workspace_id, cycle_start, requests, updated_at)" +
			" SELECT p.workspace_id, p.cycle_start, p.requests, p.updated_at FROM geocoding_usage p WHERE p.workspace_id = ?::uuid AND p.cycle_start <> ?::timestamptz" +
			" ON CONFLICT (workspace_id, cycle_start) DO UPDATE SET requests = GREATEST(m.requests, excluded.requests))",
		"taken AS (INSERT INTO geocoding_usage AS u",
		"RETURNING u.workspace_id, u.cycle_start, u.requests, u.day_requests, u.updated_at",
		"INSERT INTO geocoding_usage_months AS m (workspace_id, cycle_start, requests, updated_at) SELECT workspace_id, cycle_start, requests, updated_at FROM taken",
		"ON CONFLICT (workspace_id, cycle_start) DO UPDATE SET requests = GREATEST(m.requests, excluded.requests)",
		"SELECT requests, day_requests FROM taken",
	} {
		if !strings.Contains(takeSlotSQL, want) {
			t.Fatalf("take slot SQL misses %q: %s", want, takeSlotSQL)
		}
	}
}

func TestPlatformStatementsBindEveryPlaceholder(t *testing.T) {
	for sql, want := range map[string]int{
		readUsageOfSQL:           1,
		readHistorySQL:           4,
		coverageSQL:              strings.Count(coverageSQL, "?"),
		directoryPageSQL(false):  3,
		directoryPageSQL(true):   4,
		directoryCountSQL(false): 0,
		directoryCountSQL(true):  1,
	} {
		if got := strings.Count(sql, "?"); got != want {
			t.Errorf("%q has %d placeholders, want %d", sql, got, want)
		}
	}
	if got, want := strings.Count(coverageSQL, "?"), len(coverageArgs(pq.StringArray{"ws-1"})); got != want {
		t.Fatalf("coverage SQL has %d placeholders and %d args", got, want)
	}
}

func TestUsageOfReadsEveryWorkspaceWithOneArray(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	start := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	mock.ExpectQuery(exact(readUsageOfSQL)).WithArgs(pq.StringArray{"ws-1", "ws-2"}).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "cycle_start", "requests", "day", "day_requests"}).AddRow("ws-1", start, 12, start, 3))
	got, err := NewUsageStore(db).UsageOf(context.Background(), []string{"ws-1", "ws-2"})
	if err != nil || len(got) != 1 || got["ws-1"].Requests != 12 || got["ws-1"].DayRequests != 3 || !got["ws-1"].CycleStart.Equal(start) {
		t.Fatalf("UsageOf() = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryReadsKeptMonthsAndTheLiveCycleNotYetKept(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	since := time.Date(2025, 11, 1, 3, 0, 0, 0, time.UTC)
	oct, sep := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	ids := pq.StringArray{"ws-1", "ws-2"}
	mock.ExpectQuery(exact(readHistorySQL)).WithArgs(ids, since, ids, since).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "cycle_start", "requests"}).
			AddRow("ws-1", oct, 40).AddRow("ws-1", sep, 300).AddRow("ws-2", oct, 2))
	got, err := NewUsageStore(db).History(context.Background(), []string{"ws-1", "ws-2"}, since)
	if err != nil || len(got["ws-1"]) != 2 || got["ws-1"][1].Requests != 300 || len(got["ws-2"]) != 1 {
		t.Fatalf("History() = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FROM geocoding_usage_months WHERE workspace_id = ANY(?::uuid[]) AND cycle_start >= ?", "NOT EXISTS (SELECT 1 FROM geocoding_usage_months m WHERE m.workspace_id = u.workspace_id AND m.cycle_start = u.cycle_start)"} {
		if !strings.Contains(readHistorySQL, want) {
			t.Fatalf("history SQL misses %q", want)
		}
	}
}

func TestUsageReadsOfNoWorkspaceNeverQuery(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	store := NewUsageStore(db)
	if got, err := store.UsageOf(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("UsageOf(nil) = %+v, %v", got, err)
	}
	if got, err := store.History(context.Background(), nil, time.Now()); err != nil || len(got) != 0 {
		t.Fatalf("History(nil) = %+v, %v", got, err)
	}
	if got, err := NewPlatformReader(db).Coverage(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("Coverage(nil) = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryListsWorkspacesWithLeadsOrGeocodingByUsageThisCycle(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	cycle := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	query := geocoding.PlatformQuery{Page: 2, PageSize: 20, Search: "50%_off"}
	mock.ExpectQuery(exact(directoryCountSQL(true))).WithArgs(`%50\%\_off%`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(23))
	mock.ExpectQuery(exact(directoryPageSQL(true))).WithArgs(cycle, `%50\%\_off%`, 20, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("ws-21", "Escola").AddRow("ws-22", "Clínica"))
	got, total, err := NewPlatformReader(db).GeocodingWorkspaces(context.Background(), query, cycle)
	if err != nil || total != 23 || len(got) != 2 || got[0] != (geocoding.PlatformWorkspace{ID: "ws-21", Name: "Escola"}) {
		t.Fatalf("GeocodingWorkspaces() = %+v, %d, %v", got, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	page := directoryPageSQL(false)
	for _, want := range []string{
		"w.deleted_at IS NULL",
		"EXISTS (SELECT 1 FROM leads l WHERE l.workspace_id = w.id AND l.deleted_at IS NULL)",
		"EXISTS (SELECT 1 FROM geocoding_settings s WHERE s.workspace_id = w.id)",
		"EXISTS (SELECT 1 FROM geocoding_usage_months m WHERE m.workspace_id = w.id)",
		"LEFT JOIN geocoding_usage u ON u.workspace_id = w.id AND u.cycle_start = ?",
		"ORDER BY COALESCE(u.requests, 0) DESC, lower(w.name), w.id LIMIT ? OFFSET ?",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("directory SQL misses %q: %s", want, page)
		}
	}
	if strings.Contains(page, "ILIKE") {
		t.Fatal("a query without a search still filters by name")
	}
}

func TestDirectorySkipsThePageReadWhenNothingMatches(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(directoryCountSQL(false))).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	got, total, err := NewPlatformReader(db).GeocodingWorkspaces(context.Background(), geocoding.PlatformQuery{Page: 1, PageSize: 20}, time.Now())
	if err != nil || total != 0 || len(got) != 0 {
		t.Fatalf("GeocodingWorkspaces() = %+v, %d, %v", got, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageCountsPrimaryAddressesOfLiveLeadsPerWorkspace(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	ids := pq.StringArray{"ws-1", "ws-2"}
	args := coverageArgs(ids)
	values := make([]driver.Value, len(args))
	for i := range args {
		values[i] = sqlmock.AnyArg()
	}
	expectCoverageSession(mock)
	mock.ExpectQuery(exact(coverageSQL)).WithArgs(values...).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "total", "with_address", "on_map", "approximate", "not_found", "quota_exceeded", "refused", "pending"}).
			AddRow("ws-1", 10, 6, 3, 2, 1, 0, 0, 0))
	mock.ExpectCommit()
	got, err := NewPlatformReader(db).Coverage(context.Background(), []string{"ws-1", "ws-2"})
	if err != nil {
		t.Fatal(err)
	}
	c := got["ws-1"]
	if c.Total != 10 || c.WithoutAddress != 4 || c.OnMap != 3 || c.Approximate != 2 || c.NotFound != 1 {
		t.Fatalf("Coverage() = %+v", got)
	}
	if _, ok := got["ws-2"]; ok {
		t.Fatalf("Coverage() invented a row for a workspace without leads: %+v", got)
	}
	for _, want := range []string{
		"FROM leads LEFT JOIN lead_addresses la ON la.lead_id = leads.id AND la.is_primary AND la.workspace_id = leads.workspace_id",
		"WHERE leads.workspace_id = ANY(?::uuid[]) AND leads.deleted_at IS NULL GROUP BY leads.workspace_id",
	} {
		if !strings.Contains(coverageSQL, want) {
			t.Fatalf("coverage SQL misses %q: %s", want, coverageSQL)
		}
	}
	if args[len(args)-1].(pq.StringArray)[1] != "ws-2" {
		t.Fatal("the workspace ids are not bound last as one array")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryAnswersAnEmptyPageForTheLargestPage(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(directoryCountSQL(false))).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(23))
	got, total, err := NewPlatformReader(db).GeocodingWorkspaces(context.Background(), geocoding.PlatformQuery{Page: math.MaxInt, PageSize: geocoding.PlatformMaxPageSize}, time.Now())
	if err != nil || total != 23 || len(got) != 0 {
		t.Fatalf("GeocodingWorkspaces() of the largest page = %+v, %d, %v, want an empty page", got, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectCoverageSession(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	for _, setting := range coverageSessionSettings {
		mock.ExpectExec(exact(setting)).WillReturnResult(sqlmock.NewResult(0, 0))
	}
}

func TestCoverageRunsInABoundedReadSessionInChunksOfWorkspaces(t *testing.T) {
	for _, want := range []string{"SET LOCAL jit = off", "SET LOCAL work_mem = '32MB'", "SET LOCAL statement_timeout = '" + coverageStatementTimeout + "'"} {
		if !slices.Contains(coverageSessionSettings, want) {
			t.Fatalf("coverage session settings %q miss %q", coverageSessionSettings, want)
		}
	}
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	ids := make([]string, coverageChunk+2)
	for i := range ids {
		ids[i] = fmt.Sprintf("ws-%d", i)
	}
	rows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"workspace_id", "total", "with_address", "on_map", "approximate", "not_found", "quota_exceeded", "refused", "pending"})
	}
	chunkArgs := func(chunk []string) []driver.Value {
		args := coverageArgs(pq.StringArray(chunk))
		values := make([]driver.Value, len(args))
		for i := range args[:len(args)-1] {
			values[i] = sqlmock.AnyArg()
		}
		values[len(values)-1] = pq.StringArray(chunk)
		return values
	}
	expectCoverageSession(mock)
	mock.ExpectQuery(exact(coverageSQL)).WithArgs(chunkArgs(ids[:coverageChunk])...).WillReturnRows(rows().AddRow("ws-0", 4, 4, 4, 0, 0, 0, 0, 0))
	mock.ExpectQuery(exact(coverageSQL)).WithArgs(chunkArgs(ids[coverageChunk:])...).WillReturnRows(rows().AddRow(ids[coverageChunk+1], 2, 1, 1, 0, 0, 0, 0, 0))
	mock.ExpectCommit()
	got, err := NewPlatformReader(db).Coverage(context.Background(), ids)
	if err != nil || len(got) != 2 || got["ws-0"].OnMap != 4 || got[ids[coverageChunk+1]].WithoutAddress != 1 {
		t.Fatalf("Coverage() = %+v, %v, want both chunks read", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageFailsWholeWhenAChunkFails(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	ids := make([]string, coverageChunk+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("ws-%d", i)
	}
	expectCoverageSession(mock)
	mock.ExpectQuery(exact(coverageSQL)).WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "total"}).AddRow("ws-0", 1))
	mock.ExpectQuery(exact(coverageSQL)).WillReturnError(errors.New("ERROR: canceling statement due to statement timeout (SQLSTATE 57014)"))
	mock.ExpectRollback()
	got, err := NewPlatformReader(db).Coverage(context.Background(), ids)
	if !errors.Is(err, context.DeadlineExceeded) || got != nil {
		t.Fatalf("Coverage() = %+v, %v, want no partial answer and a deadline", got, err)
	}
}
