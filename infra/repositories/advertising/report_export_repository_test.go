package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

func TestReportExportListLeavesTheFileOut(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, workspace_id, ad_account_id, report_id, name, since, until, rows, size_bytes, created_by, created_at FROM "ad_report_exports" WHERE workspace_id = $1 ORDER BY created_at DESC, id LIMIT $2`)).
		WithArgs("ws", 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "rows", "size_bytes"}).AddRow("e-1", "ws", "Mensal", 3, 120))
	list, err := NewReportExportRepository(db).List(context.Background(), "ws", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SizeBytes != 120 || list[0].Content != nil {
		t.Fatalf("got %+v", list)
	}
	expectationsMet(t, mock)
}

func TestReportExportKeepsOnlyTheNewest(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ad_report_exports" WHERE workspace_id = $1 AND id NOT IN (SELECT "id" FROM "ad_report_exports" WHERE workspace_id = $2 ORDER BY created_at DESC, id LIMIT $3)`)).
		WithArgs("ws", "ws", 100).WillReturnResult(sqlmock.NewResult(0, 4))
	if err := NewReportExportRepository(db).KeepNewest(context.Background(), "ws", 100); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestReportExportDeleteOfAMissingFile(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "ad_report_exports"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewReportExportRepository(db).Delete(context.Background(), "ws", "e-1"); !errors.Is(err, advertising.ErrReportExportNotFound) {
		t.Fatalf("got %v", err)
	}
}
