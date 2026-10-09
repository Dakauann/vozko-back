package geocoding_repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/geocoding"
)

func TestPlatformStatementsBindEveryPlaceholder(t *testing.T) {
	for sql, want := range map[string]int{
		coverageSQL:              strings.Count(coverageSQL, "?"),
		directoryPageSQL(false):  2,
		directoryPageSQL(true):   3,
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

func TestDirectorySkipsThePageReadWhenNothingMatches(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(directoryCountSQL(false))).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	got, total, err := NewPlatformReader(db).GeocodingWorkspaces(context.Background(), geocoding.PlatformQuery{Page: 1, PageSize: 20})
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
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "total", "with_address", "on_map", "approximate", "not_found", "pending"}).
			AddRow("ws-1", 10, 6, 3, 2, 1, 0))
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
	got, total, err := NewPlatformReader(db).GeocodingWorkspaces(context.Background(), geocoding.PlatformQuery{Page: math.MaxInt, PageSize: geocoding.PlatformMaxPageSize})
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
		return sqlmock.NewRows([]string{"workspace_id", "total", "with_address", "on_map", "approximate", "not_found", "pending"})
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
	mock.ExpectQuery(exact(coverageSQL)).WithArgs(chunkArgs(ids[:coverageChunk])...).WillReturnRows(rows().AddRow("ws-0", 4, 4, 4, 0, 0, 0))
	mock.ExpectQuery(exact(coverageSQL)).WithArgs(chunkArgs(ids[coverageChunk:])...).WillReturnRows(rows().AddRow(ids[coverageChunk+1], 2, 1, 1, 0, 0, 0))
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

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func exact(query string) string {
	quoted := regexp.QuoteMeta(query)
	var b strings.Builder
	n := 0
	for {
		before, after, found := strings.Cut(quoted, `\?`)
		b.WriteString(before)
		if !found {
			break
		}
		n++
		b.WriteString(`\$` + strconv.Itoa(n))
		quoted = after
	}
	return "^" + b.String() + "$"
}

func TestCoverageOfNoWorkspaceNeverQueries(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	if got, err := NewPlatformReader(db).Coverage(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("Coverage(nil) = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryListsLiveWorkspacesWithLiveLeadsByName(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	query := geocoding.PlatformQuery{Page: 2, PageSize: 20, Search: "50%_off"}
	mock.ExpectQuery(exact(directoryCountSQL(true))).WithArgs(`%50\%\_off%`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(23))
	mock.ExpectQuery(exact(directoryPageSQL(true))).WithArgs(`%50\%\_off%`, 20, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("ws-21", "Escola").AddRow("ws-22", "Clínica"))
	got, total, err := NewPlatformReader(db).GeocodingWorkspaces(context.Background(), query)
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
		"ORDER BY lower(w.name), w.id LIMIT ? OFFSET ?",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("directory SQL misses %q: %s", want, page)
		}
	}
	for _, retired := range []string{"ILIKE", "geocoding_settings", "geocoding_usage"} {
		if strings.Contains(page, retired) {
			t.Fatalf("directory SQL without a search still reads %q: %s", retired, page)
		}
	}
}
