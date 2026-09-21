package unofficial_whatsapp_repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/export"
)

type capturedSQL struct{ statements []string }

func (c *capturedSQL) matcher() sqlmock.QueryMatcher {
	return sqlmock.QueryMatcherFunc(func(_, actual string) error {
		c.statements = append(c.statements, actual)
		return nil
	})
}

func (c *capturedSQL) last() string {
	if len(c.statements) == 0 {
		return ""
	}
	return c.statements[len(c.statements)-1]
}

func newExportDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *capturedSQL, *sql.DB) {
	t.Helper()
	captured := &capturedSQL{}
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captured.matcher()))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, captured, sqlDB
}

func campaignEntryRows() *sqlmock.Rows {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"conversation_id", "status", "error_code", "error_message",
		"created_at", "updated_at", "number", "entry_name",
		"contact_name", "verified_name", "profile_name",
	}).
		AddRow("", "FAILED", 1001, "message not sent", at, at, "5511900000001", "Ana Importada", "", "", "").
		AddRow("conv-2", "SENT", 0, "", at, at, "5511900000002", "Bruno", "Bruno Silva", "", "")
}

func campaignScope() export.Scope {
	return export.Scope{WorkspaceID: "ws-1", ContainerID: "camp-1", ContainerType: "campaign"}
}

func collect(t *testing.T, db *gorm.DB, scope export.Scope) []export.ChannelEntry {
	t.Helper()
	var got []export.ChannelEntry
	err := NewExportRepository(db).ListForExport(context.Background(), scope, func(e export.ChannelEntry) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("list for export: %v", err)
	}
	return got
}

func TestCampaignExportWalksTargetsAndCarriesTheFailure(t *testing.T) {
	db, mock, captured, sqlDB := newExportDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery("").WillReturnRows(campaignEntryRows())

	got := collect(t, db, campaignScope())

	if !strings.Contains(captured.last(), "unofficial_whatsapp_campaign_entries") {
		t.Errorf("campaign export does not read the targets table:\n%s", captured.last())
	}
	if len(got) != 2 {
		t.Fatalf("emitted %d rows, want 2. The failed target belongs in the file", len(got))
	}

	failed := got[0]
	if failed.Status != "FAILED" {
		t.Errorf("status is %q, want the SEND status FAILED", failed.Status)
	}
	if failed.FailureCode != 1001 || failed.FailureReason != "message not sent" {
		t.Errorf("failure is %d/%q", failed.FailureCode, failed.FailureReason)
	}
	if failed.Number != "+5511900000001" {
		t.Errorf("number is %q", failed.Number)
	}
	if failed.EntryID != "" {
		t.Errorf("entry id is %q, want empty for a target with no conversation", failed.EntryID)
	}
	if failed.Name != "Ana Importada" {
		t.Errorf("name is %q", failed.Name)
	}

	if got[1].Name != "Bruno Silva" {
		t.Errorf("sent row name is %q, want the contact name", got[1].Name)
	}
	if got[1].EntryID != "conv-2" {
		t.Errorf("sent row entry id is %q", got[1].EntryID)
	}
}

func TestCampaignExportScopesTenancyStatusAndDepartment(t *testing.T) {
	db, mock, captured, sqlDB := newExportDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery("").WillReturnRows(campaignEntryRows())

	scope := campaignScope()
	scope.Statuses = []string{"FAILED"}
	scope.DepartmentIDs = []string{"dept-1"}
	collect(t, db, scope)

	sql := captured.last()
	for _, want := range []string{
		"uwce.workspace_id",
		"uwce.campaign_id",
		"uwce.status IN",
		"uwcp.department_id IN",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("query is missing %q:\n%s", want, sql)
		}
	}
}

func TestNumberExportStillWalksConversations(t *testing.T) {
	db, mock, captured, sqlDB := newExportDB(t)
	defer sqlDB.Close()

	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
		"entry_id", "status", "created_at", "updated_at",
		"phone_number", "jid", "contact_name", "verified_name", "name",
	}).AddRow("conv-1", "open", at, at, "5511900000003", "", "Carla", "", ""))

	got := collect(t, db, export.Scope{WorkspaceID: "ws-1", ContainerID: "inst-1"})

	if !strings.Contains(captured.last(), "unofficial_whatsapp_conversations") {
		t.Errorf("number export does not read conversations:\n%s", captured.last())
	}
	if len(got) != 1 || got[0].Status != "open" {
		t.Fatalf("emitted %#v", got)
	}
	if got[0].FailureCode != 0 || got[0].FailureReason != "" {
		t.Errorf("conversation row carries a failure: %d/%q", got[0].FailureCode, got[0].FailureReason)
	}
}
