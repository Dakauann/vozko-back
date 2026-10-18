package calllist_repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/calls/calllist"
)

func TestAddingOneLeadAppendsItOnceAfterTheLastPositionAgainstPostgres(t *testing.T) {
	f := newFixture(t, 3)
	ctx := context.Background()
	newcomer := uuid.NewString()
	if err := f.db.Exec("INSERT INTO leads (id, workspace_id, number, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		newcomer, f.workspace, "5511912340000", "Nova", now, now).Error; err != nil {
		t.Fatal(err)
	}
	add := calllist.Addition{WorkspaceID: f.workspace, ListID: f.listID, At: now, Items: []calllist.Item{{LeadID: newcomer, Phone: "5511912340000"}}}

	added, err := f.store.AddItems(ctx, add)
	if err != nil || added != 1 {
		t.Fatalf("first add = %d, %v", added, err)
	}
	again, err := f.store.AddItems(ctx, add)
	if err != nil || again != 0 {
		t.Fatalf("second add = %d, %v", again, err)
	}
	existing, err := f.store.AddItems(ctx, calllist.Addition{WorkspaceID: f.workspace, ListID: f.listID, At: now,
		Items: []calllist.Item{{LeadID: f.leads[0], Phone: "5511987654321"}}})
	if err != nil || existing != 0 {
		t.Fatalf("a lead the build already added = %d, %v", existing, err)
	}

	l, err := f.store.Get(ctx, f.workspace, f.listID)
	if err != nil || l.ItemCount != 4 {
		t.Fatalf("list = %+v, %v, want 4 items", l, err)
	}
	item := f.itemOfLead(t, newcomer)
	if item.Position != 4 || item.State != calllist.StatePending || item.Phone != "5511912340000" {
		t.Fatalf("added item = %+v", item)
	}
}

func TestAnArchivedListTakesNoNewLeadAgainstPostgres(t *testing.T) {
	f := newFixture(t, 1)
	ctx := context.Background()
	if err := f.db.Exec("UPDATE call_lists SET status = 'archived' WHERE id = ?", f.listID).Error; err != nil {
		t.Fatal(err)
	}
	_, err := f.store.AddItems(ctx, calllist.Addition{WorkspaceID: f.workspace, ListID: f.listID, At: now,
		Items: []calllist.Item{{LeadID: uuid.NewString(), Phone: "5511912340000"}}})
	if !errors.Is(err, calllist.ErrListNotActive) {
		t.Fatalf("add = %v, want ErrListNotActive", err)
	}
	var count int64
	f.db.Raw("SELECT COUNT(*) FROM call_list_items WHERE list_id = ?", f.listID).Scan(&count)
	if count != 1 {
		t.Fatalf("items = %d, want the list untouched", count)
	}
}
