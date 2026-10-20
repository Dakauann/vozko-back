package mercadolivre_repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/export"
)

func TestExportStreamsBoundedPagesWithAKeyset(t *testing.T) {
	db, mock, _ := newMockDB(t)
	repo := &exportRepository{db: db, pageSize: 2}
	created := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	columns := []string{"entry_id", "status", "created_at", "updated_at", "nickname", "ml_user_id", "pack_key"}

	mock.ExpectQuery(`FROM mercadolivre_conversations mlc JOIN mercadolivre_contacts mlct .* WHERE mlc.workspace_id = \$1 AND mlc.deleted_at IS NULL AND mlc.account_id = \$2 ORDER BY mlc.created_at DESC,mlc.id DESC LIMIT \$3`).
		WithArgs("ws-1", "a-1", 2).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow("c-3", "new", created, created, "COMPRADOR", "111", "2000000000000003").
			AddRow("c-2", "ongoing", created, created, "", "222", "2000000000000002"))
	mock.ExpectQuery(`AND \(mlc.created_at, mlc.id\) < \(\$3, \$4\) ORDER BY mlc.created_at DESC,mlc.id DESC LIMIT \$5`).
		WithArgs("ws-1", "a-1", created, "c-2", 2).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("c-1", "finished", created, created, "OUTRO", "333", "2000000000000001"))

	var got []export.ChannelEntry
	err := repo.ListForExport(context.Background(), export.Scope{WorkspaceID: "ws-1", ContainerID: "a-1"}, func(e export.ChannelEntry) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "COMPRADOR" || got[1].Name != "222" || got[2].Status != "finished" {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].Number != "2000000000000003" {
		t.Fatalf("the pack identifies the post-sale conversation: %+v", got[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExportWithoutAWorkspaceReadsNothing(t *testing.T) {
	db, _, _ := newMockDB(t)
	if err := NewExportRepository(db).ListForExport(context.Background(), export.Scope{}, func(export.ChannelEntry) error {
		t.Fatal("emitted without a workspace")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
