package report_repository

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/report"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestListCountsAndPagesOnlyTheJobsTheViewerMayRead(t *testing.T) {
	db, mock := mockDB(t)
	visible := regexp.QuoteMeta("((requested_by = $2 OR (readers IS NOT NULL AND cardinality(readers) > 0 AND readers <@ $3::text[])))")
	mock.ExpectQuery(`SELECT count\(\*\) FROM "report_jobs" WHERE workspace_id = \$1 AND `+visible).
		WithArgs("ws-1", "viewer-1", `{"leads:read"}`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(`SELECT \* FROM "report_jobs" WHERE workspace_id = \$1 AND `+visible+` ORDER BY created_at DESC LIMIT \$4 OFFSET \$5`).
		WithArgs("ws-1", "viewer-1", `{"leads:read"}`, 2, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "kind", "readers"}).AddRow("job-3", "ws-1", "leads", `{"leads:read"}`))

	page, err := New(db).List(report.ListQuery{WorkspaceID: "ws-1", Viewer: "viewer-1", ViewerHolds: []string{"leads:read"}, Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Jobs) != 1 || len(page.Jobs[0].Readers) != 1 {
		t.Fatalf("page = %+v", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListWithoutAViewerReadsNothing(t *testing.T) {
	db, mock := mockDB(t)
	page, err := New(db).List(report.ListQuery{WorkspaceID: "ws-1"})
	if err != nil || len(page.Jobs) != 0 || page.Total != 0 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReaderKeysAreTheDistinctKeysOfTheWorkspaceJobs(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT unnest(readers) AS reader FROM report_jobs WHERE workspace_id = $1 AND readers IS NOT NULL")).
		WithArgs("ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"reader"}).AddRow("leads:read").AddRow("balance:read"))
	keys, err := New(db).ReaderKeys("ws-1")
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys = %v, %v", keys, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
