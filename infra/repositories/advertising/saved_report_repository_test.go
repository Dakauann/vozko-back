package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

func TestSavedReportCreateAssignsTheID(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "ad_saved_reports"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	report := &advertising.SavedReport{WorkspaceID: "ws", AdAccountID: "a-1", Name: "Mensal", CreatedBy: "u-1"}
	if err := NewSavedReportRepository(db).Create(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if report.ID == "" {
		t.Fatal("no id")
	}
	expectationsMet(t, mock)
}

func TestSavedReportFindIsScopedAndMapsTheDefinition(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_saved_reports" WHERE workspace_id = $1 AND id = $2`)).
		WithArgs("ws", "r-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "definition"}).
			AddRow("r-1", "ws", "Mensal", []byte(`{"view":"bars","level":"ad","metrics":["spend"],"datePreset":"last7"}`)))
	report, err := NewSavedReportRepository(db).Find(context.Background(), "ws", "r-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.Definition.View != advertising.ViewBars || report.Definition.Metrics[0] != advertising.ReportSpend {
		t.Fatalf("got %+v", report)
	}
}

func TestSavedReportSaveOfAMissingReport(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "ad_saved_reports" SET`)).WillReturnResult(sqlmock.NewResult(0, 0))
	err := NewSavedReportRepository(db).Save(context.Background(), &advertising.SavedReport{ID: "r-1", WorkspaceID: "ws"})
	if !errors.Is(err, advertising.ErrReportNotFound) {
		t.Fatalf("got %v", err)
	}
}
