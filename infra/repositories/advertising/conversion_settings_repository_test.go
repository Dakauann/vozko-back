package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

func TestConversionSettingsGetMissingIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_conversion_settings" WHERE workspace_id = $1`)).
		WithArgs("ws", 1).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}))
	if _, err := NewConversionSettingsRepository(db).Get(context.Background(), "ws"); !errors.Is(err, advertising.ErrSettingsNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversionSettingsGetMapsTheRow(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "ad_conversion_settings"`).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "ad_account_id", "dataset_id", "pixel_id", "send_leads", "send_purchases", "enabled"}).
			AddRow("ws", "a-1", "ds-1", "px-1", true, false, true))
	s, err := NewConversionSettingsRepository(db).Get(context.Background(), "ws")
	if err != nil {
		t.Fatal(err)
	}
	want := advertising.ConversionSettings{WorkspaceID: "ws", AdAccountID: "a-1", DatasetID: "ds-1", PixelID: "px-1", SendLeads: true, Enabled: true}
	if *s != want {
		t.Fatalf("got %+v", s)
	}
}

func TestConversionSettingsSaveUpsertsByWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO ad_conversion_settings .* ON CONFLICT \(workspace_id\) DO UPDATE SET .* RETURNING updated_at`).
		WithArgs("ws", "a-1", "ds-1", "", true, true, true).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	s := &advertising.ConversionSettings{WorkspaceID: "ws", AdAccountID: "a-1", DatasetID: "ds-1", SendLeads: true, SendPurchases: true, Enabled: true}
	if err := NewConversionSettingsRepository(db).Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !s.UpdatedAt.Equal(now) {
		t.Fatalf("got %+v", s)
	}
	expectationsMet(t, mock)
}

func TestConversionSettingsRequireWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewConversionSettingsRepository(db)
	if _, err := repo.Get(context.Background(), " "); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("get: %v", err)
	}
	if err := repo.Save(context.Background(), &advertising.ConversionSettings{AdAccountID: "a-1"}); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("save: %v", err)
	}
	expectationsMet(t, mock)
}

func TestConversionSettingsListEnabled(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_conversion_settings" WHERE enabled = $1 ORDER BY workspace_id`)).
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "enabled"}).AddRow("ws-1", true).AddRow("ws-2", true))
	list, err := NewConversionSettingsRepository(db).ListEnabled(context.Background())
	if err != nil || len(list) != 2 || list[1].WorkspaceID != "ws-2" {
		t.Fatalf("got %v, %v", list, err)
	}
}
