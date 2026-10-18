package buttonaction_repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/buttonaction"
)

const (
	workspaceA = "6f1d1c52-2a6c-4b8e-9f43-1c1f5b7d2a01"
	campaignA  = "0b5a2f7e-8d4c-4e1a-9c2b-3f6e7d8a9b10"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestEveryStatementBindsAsManyArgumentsAsItHasPlaceholders(t *testing.T) {
	cases := map[string]struct {
		sql  string
		args int
	}{
		"select template exit":    {selectTemplateExitSQL, 3},
		"select template actions": {selectTemplateActionsSQL, 3},
		"insert template exit":    {insertTemplateExitSQL, 5},
		"update template exit":    {updateTemplateExitSQL, 6},
		"delete template actions": {deleteTemplateActionsSQL, 3},
		"select campaign routes":  {selectCampaignRoutesSQL, 2},
		"insert campaign routes":  {insertCampaignRoutesSQL, 8},
		"update campaign routes":  {updateCampaignRoutesSQL, 9},
	}
	for name, tc := range cases {
		if got := strings.Count(tc.sql, "?"); got != tc.args {
			t.Errorf("%s: %d placeholders, the call binds %d", name, got, tc.args)
		}
	}
}

func TestTemplateSettingsWithNothingStoredStartAtTheFirstVersion(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM template_exit_settings`).WithArgs(workspaceA, "promo", "pt_BR").
		WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectQuery(`FROM template_button_actions`).WithArgs(workspaceA, "promo", "pt_BR").
		WillReturnRows(sqlmock.NewRows([]string{"button_index"}))

	got, err := NewTemplateSettingsStore(db).Get(context.Background(), workspaceA, buttonaction.TemplateRef{Name: "promo", Language: "pt_BR"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != buttonaction.FirstVersion || got.Actions == nil || len(got.Actions) != 0 || got.ExitConfirmation != "" {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateSettingsSaveWithAStaleVersionConflicts(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE template_exit_settings SET`).WillReturnRows(sqlmock.NewRows([]string{"version", "updated_at"}))
	mock.ExpectRollback()

	settings := &buttonaction.TemplateSettings{WorkspaceID: workspaceA, Template: buttonaction.TemplateRef{Name: "promo", Language: "pt_BR"}}
	err := NewTemplateSettingsStore(db).Save(context.Background(), settings, 4)
	if !errors.Is(err, buttonaction.ErrVersionConflict) {
		t.Fatalf("got %v, want ErrVersionConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoresRefuseAnInvalidWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	ref := buttonaction.TemplateRef{Name: "promo", Language: "pt_BR"}
	if _, err := NewTemplateSettingsStore(db).Get(context.Background(), "ws", ref); !errors.Is(err, buttonaction.ErrWorkspaceRequired) {
		t.Fatalf("template get: %v", err)
	}
	if _, err := NewCampaignRoutesStore(db).Get(context.Background(), workspaceA, "nope"); !errors.Is(err, buttonaction.ErrCampaignNotFound) {
		t.Fatalf("campaign get: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCampaignRoutesFirstSaveThatLosesTheRaceConflicts(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`INSERT INTO whatsapp_campaign_button_routes`).WillReturnRows(sqlmock.NewRows([]string{"version", "updated_at"}))

	routes := &buttonaction.CampaignRoutes{WorkspaceID: workspaceA, CampaignID: campaignA, Template: buttonaction.TemplateRef{Name: "promo", Language: "pt_BR"}}
	if err := NewCampaignRoutesStore(db).Save(context.Background(), routes, buttonaction.FirstVersion); !errors.Is(err, buttonaction.ErrVersionConflict) {
		t.Fatalf("got %v, want ErrVersionConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
