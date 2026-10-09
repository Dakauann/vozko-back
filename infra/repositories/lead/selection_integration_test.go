package lead

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func selectionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDBWith(t, "lead_selection", repotest.Options{PrepareStmt: true})
	migrateLeadTables(t, db)
	if err := db.AutoMigrate(&schema.LeadSelectionSnapshot{}, &schema.LeadActionRun{}, &schema.ReportJob{}, &schema.LeadMemory{}, &schema.LeadMessageWindow{}, &schema.CallList{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedLeads(t *testing.T, db *gorm.DB, workspaceID string, n int) []string {
	t.Helper()
	var ids []string
	err := db.Raw(`INSERT INTO leads (id, workspace_id, name, blocked, version, created_at, updated_at)
		SELECT gen_random_uuid(), ?::uuid, 'Lead ' || lpad(g::text, 5, '0'), g % 10 = 0, 1, now() - (g || ' minutes')::interval, now()
		FROM generate_series(1, ?) g RETURNING id::text`, workspaceID, n).Scan(&ids).Error
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	return ids
}

func TestTheLeadSelectionKeysetsFreezesAndCountsAgainstPostgres(t *testing.T) {
	db := selectionDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	ids := seedLeads(t, db, ws, 1200)
	seedLeads(t, db, other, 30)
	if err := db.Exec("UPDATE leads SET deleted_at = now() WHERE id = ?", ids[0]).Error; err != nil {
		t.Fatal(err)
	}

	count, err := repo.CountSelection(ctx, lead.SelectionQuery{WorkspaceID: ws})
	if err != nil || count != 1199 {
		t.Fatalf("count = %d, %v", count, err)
	}
	excluded := ids[5]
	q := lead.SelectionQuery{WorkspaceID: ws, ExcludeIDs: []string{excluded}, Require: notBlocked()}
	var paged []string
	after := ""
	for {
		page, err := repo.SelectionPage(ctx, q, after, 500)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		paged = append(paged, page...)
		after = page[len(page)-1]
	}
	if !sort.StringsAreSorted(paged) {
		t.Fatal("the keyset pages are not in id order")
	}
	var unblocked int
	db.Raw("SELECT COUNT(*) FROM leads WHERE workspace_id = ? AND deleted_at IS NULL AND NOT blocked AND id <> ?", ws, excluded).Scan(&unblocked)
	if len(paged) != unblocked {
		t.Fatalf("paged %d leads, want %d", len(paged), unblocked)
	}

	firstN := lead.SelectionQuery{WorkspaceID: ws, Require: notBlocked(), Order: []shared.Sort{{Field: string(lead.SortName), Direction: shared.SortAsc}}, Limit: 50}
	head, err := repo.SelectionPage(ctx, firstN, "", 500)
	if err != nil || len(head) != 50 {
		t.Fatalf("first n = %d, %v", len(head), err)
	}
	var expected []string
	db.Raw("SELECT id::text FROM (SELECT id FROM leads WHERE workspace_id = ? AND deleted_at IS NULL AND NOT blocked ORDER BY name, id DESC LIMIT 50) x ORDER BY id", ws).Scan(&expected)
	if len(expected) != 50 || expected[0] != head[0] || expected[49] != head[49] {
		t.Fatal("first n is not the first fifty unblocked leads by name")
	}

	snapshot := uuid.NewString()
	frozen, err := repo.FreezeSelection(ctx, firstN, snapshot)
	if err != nil || frozen.Size != 50 || frozen.Existed {
		t.Fatalf("freeze = %+v, %v", frozen, err)
	}
	again, err := repo.FreezeSelection(ctx, q, snapshot)
	if err != nil || again.Size != 50 || !again.Existed {
		t.Fatalf("a second freeze of the same snapshot = %+v, %v", again, err)
	}
	firstPage, _ := repo.SnapshotPage(ctx, ws, snapshot, "", 30)
	rest, _ := repo.SnapshotPage(ctx, ws, snapshot, firstPage[len(firstPage)-1], 30)
	if len(firstPage) != 30 || len(rest) != 20 || firstPage[0] != head[0] {
		t.Fatalf("snapshot pages = %d and %d", len(firstPage), len(rest))
	}
	if foreign, _ := repo.SnapshotPage(ctx, other, snapshot, "", 30); len(foreign) != 0 {
		t.Fatal("another workspace read the snapshot")
	}
	if err := repo.DropSnapshot(ctx, ws, snapshot); err != nil {
		t.Fatal(err)
	}
	if left, _ := repo.SnapshotPage(ctx, ws, snapshot, "", 30); len(left) != 0 {
		t.Fatal("a dropped snapshot still holds leads")
	}
	old := uuid.NewString()
	if _, err := repo.FreezeSelection(ctx, lead.SelectionQuery{WorkspaceID: ws, IDs: ids[1:4]}, old); err != nil {
		t.Fatal(err)
	}
	if swept, err := repo.DropSnapshotsBefore(ctx, time.Now().Add(time.Hour), 2); err != nil || swept != 2 {
		t.Fatalf("sweep = %d, %v", swept, err)
	}
}

func TestBulkEditsWriteVersionsAndEventsOnceAgainstPostgres(t *testing.T) {
	db := selectionDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws := uuid.NewString()
	actor := uuid.NewString()
	ids := seedLeads(t, db, ws, 600)
	batch := ids[:500]
	classify := leadaction.Edit{Kind: leadaction.EditCustomField, Field: lead.CustomFieldName("interesse"), Key: "interesse", Value: "alto", Recorded: true, EventKind: lead.EventUpdated}

	tally, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: actor, LeadIDs: batch, Edit: classify, At: time.Now()})
	if err != nil || len(tally.Changed) != 500 || tally.Unchanged != 0 {
		t.Fatalf("first classification = %d changed, %d unchanged, %v", len(tally.Changed), tally.Unchanged, err)
	}
	again, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: actor, LeadIDs: batch, Edit: classify, At: time.Now()})
	if err != nil || len(again.Changed) != 0 || again.Unchanged != 500 {
		t.Fatalf("the same classification again = %+v, %v", again, err)
	}
	var row struct {
		Version      int64
		CustomFields []byte
	}
	db.Raw("SELECT version, custom_fields FROM leads WHERE id = ?", batch[0]).Scan(&row)
	var fields map[string]any
	_ = json.Unmarshal(row.CustomFields, &fields)
	if row.Version != 2 || fields["interesse"] != "alto" {
		t.Fatalf("lead after classification = version %d, %s", row.Version, row.CustomFields)
	}
	var events int64
	db.Raw("SELECT COUNT(*) FROM lead_events WHERE workspace_id = ? AND kind = 'updated'", ws).Scan(&events)
	if events != 500 {
		t.Fatalf("events = %d, want one per changed lead", events)
	}

	clear := classify
	clear.Value = nil
	if _, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: actor, LeadIDs: batch[:1], Edit: clear, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var cleared bool
	db.Raw("SELECT custom_fields IS NULL FROM leads WHERE id = ?", batch[0]).Scan(&cleared)
	if !cleared {
		t.Fatal("clearing the only field must leave no custom fields")
	}

	owner := leadaction.Edit{Kind: leadaction.EditOwner, Field: lead.FieldOwner, Value: actor, Recorded: true, EventKind: lead.EventOwnerChange}
	if tally, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: actor, LeadIDs: ids[500:], Edit: owner, At: time.Now()}); err != nil || len(tally.Changed) != 100 {
		t.Fatalf("owner = %+v, %v", tally, err)
	}
	block := leadaction.Edit{Kind: leadaction.EditBlocked, Field: lead.FieldBlocked, Value: true, Recorded: true, EventKind: lead.EventBlocked}
	blocked, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: actor, LeadIDs: ids[500:], Edit: block, At: time.Now()})
	if err != nil || len(blocked.Changed)+blocked.Unchanged != 100 || blocked.Unchanged == 0 {
		t.Fatalf("block = %+v, %v", blocked, err)
	}
	preview, err := repo.TallyBatch(ctx, ws, ids, block)
	if err != nil || preview.Unchanged < 100 {
		t.Fatalf("tally = %+v, %v", preview, err)
	}
	if err := db.Exec("UPDATE leads SET number = '5531900000001' WHERE id = ?", ids[599]).Error; err != nil {
		t.Fatal(err)
	}
	targets, err := repo.BlockTargets(ctx, ws, ids[500:], true)
	if err != nil || len(targets) != 1 || targets[0].LeadID != ids[599] {
		t.Fatalf("block targets = %+v, %v", targets, err)
	}
}
