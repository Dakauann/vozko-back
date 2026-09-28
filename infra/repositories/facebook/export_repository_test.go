package facebook_repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/export"
)

func TestExportListsPageConversationsWithTheDomainDisplayName(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM facebook_conversations fbc JOIN facebook_contacts fbct ON fbct.id = fbc.contact_id AND fbct.deleted_at IS NULL WHERE fbc.workspace_id = \$1 AND fbc.deleted_at IS NULL AND fbc.page_id = \$2 ORDER BY fbc.created_at DESC`).
		WithArgs("ws-1", "page-1").
		WillReturnRows(sqlmock.NewRows([]string{"entry_id", "status", "created_at", "updated_at", "name", "first_name", "last_name", "psid"}).
			AddRow("conv-1", "ongoing", at, at, "", "Ana", "Souza", "123456789").
			AddRow("conv-2", "new", at, at, "", "", "", "987654321"))

	var got []export.ChannelEntry
	err := NewExportRepository(db).ListForExport(context.Background(),
		export.Scope{WorkspaceID: "ws-1", ContainerID: "page-1"},
		func(e export.ChannelEntry) error { got = append(got, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows", len(got))
	}
	if got[0].Name != "Ana Souza" || got[0].Number != "123456789" || got[0].Status != "ongoing" {
		t.Errorf("first row = %+v", got[0])
	}
	if got[1].Name != "Facebook user 4321" {
		t.Errorf("unnamed contact = %q, want the domain fallback", got[1].Name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExportWithoutAWorkspaceReadsNothing(t *testing.T) {
	db, _, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	err := NewExportRepository(db).ListForExport(context.Background(), export.Scope{},
		func(export.ChannelEntry) error { t.Fatal("emitted without a workspace"); return nil })
	if err != nil {
		t.Fatal(err)
	}
}
