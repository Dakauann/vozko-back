package tiktok_repository

import (
	"context"
	"testing"

	"vozko/domain/export"
)

func TestExportStreamsEveryConversationOfTheAccountInKeysetPages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.conversation(t, "conv-1")
	second := f.conversation(t, "conv-2")
	third := f.conversation(t, "conv-3")
	if err := f.db.Exec("UPDATE tiktok_contacts SET display_name = 'Maria', username = 'maria.loja' WHERE id = ?", first.ContactID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("UPDATE tiktok_contacts SET username = 'joao' WHERE id = ?", second.ContactID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("UPDATE tiktok_conversations SET deleted_at = NOW() WHERE id = ?", third.ID).Error; err != nil {
		t.Fatal(err)
	}

	repo := &exportRepository{db: f.db, pageSize: 1}
	var got []export.ChannelEntry
	err := repo.ListForExport(ctx, export.Scope{WorkspaceID: f.account.WorkspaceID, ContainerID: f.account.ID}, func(e export.ChannelEntry) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]export.ChannelEntry{}
	for _, e := range got {
		byID[e.EntryID] = e
	}
	if len(got) != 2 || len(byID) != 2 {
		t.Fatalf("exported %+v, want the two live conversations once each", got)
	}
	if e := byID[first.ID]; e.Name != "Maria" || e.Number != "@maria.loja" || e.Status != "new" {
		t.Fatalf("first = %+v", e)
	}
	if e := byID[second.ID]; e.Name != "joao" || e.Number != "@joao" {
		t.Fatalf("second = %+v", e)
	}

	var foreign []export.ChannelEntry
	if err := repo.ListForExport(ctx, export.Scope{WorkspaceID: "00000000-0000-0000-0000-000000000000"}, func(e export.ChannelEntry) error {
		foreign = append(foreign, e)
		return nil
	}); err != nil || len(foreign) != 0 {
		t.Fatalf("another workspace exported %+v, %v", foreign, err)
	}
}
