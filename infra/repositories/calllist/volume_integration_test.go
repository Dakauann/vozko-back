package calllist_repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/calls/calllist"
	"vozko/infra/database"
)

func TestTheQueueStaysFastOnAListAtItsCapAgainstPostgres(t *testing.T) {
	f := newFixture(t, 1)
	ctx := context.Background()
	if err := f.db.Exec(`INSERT INTO call_list_items (id, workspace_id, list_id, lead_id, phone, position, state, callback_at, created_at, updated_at)
		SELECT gen_random_uuid(), ?::uuid, ?::uuid, gen_random_uuid(), '5511987654321', g + 1,
			CASE WHEN g < ? THEN 'closed' ELSE 'pending' END,
			CASE WHEN g % 97 = 0 THEN ?::timestamptz + interval '1 day' WHEN g % 89 = 0 THEN ?::timestamptz - interval '1 hour' END, ?, ?
		FROM generate_series(1, ?) g`, f.workspace, f.listID, calllist.MaxItems/2, now, now, now.Add(-time.Hour), now, calllist.MaxItems-1).Error; err != nil {
		t.Fatal(err)
	}
	for _, seed := range []string{
		`INSERT INTO leads (id, workspace_id, number, name, created_at, updated_at)
			SELECT i.lead_id, i.workspace_id, '5511' || lpad(i.position::text, 9, '0'), 'Pessoa ' || i.position, i.created_at, i.created_at
			FROM call_list_items i WHERE i.list_id = ?::uuid AND i.position > 1`,
		`INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, district, city, geo_status, fingerprint, created_at, updated_at)
			SELECT gen_random_uuid(), i.workspace_id, i.lead_id, 'home', true, 'Bairro ' || (i.position % 300), 'Barueri', 'pending', 'f' || i.position, i.created_at, i.created_at
			FROM call_list_items i WHERE i.list_id = ?::uuid AND i.position > 1`,
		`INSERT INTO calls (id, call_id, workspace_id, type, direction, source, status, lead_id, started_at, created_at, updated_at)
			SELECT gen_random_uuid(), 'sip-' || i.id, i.workspace_id, 'crm', 'outbound', 'sip_trunk', 'completed', i.lead_id, i.created_at, i.created_at, i.created_at
			FROM call_list_items i WHERE i.list_id = ?::uuid AND i.position % 3 = 0`,
		`UPDATE call_list_items i SET last_call_id = c.id FROM calls c
			WHERE i.list_id = ?::uuid AND c.call_id = 'sip-' || i.id`,
	} {
		started := time.Now()
		if err := f.db.Exec(seed, f.listID).Error; err != nil {
			t.Fatal(err)
		}
		t.Logf("seeded in %v: %.60s", time.Since(started), seed)
	}
	for _, table := range []string{"leads", "lead_addresses", "calls"} {
		if err := f.db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	agenda, ok := database.ConcurrentIndexSQL(database.CallListAgendaIndex)
	if !ok {
		t.Fatalf("%s is not built", database.CallListAgendaIndex)
	}
	if err := f.db.Exec(agenda).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("ANALYZE call_list_items").Error; err != nil {
		t.Fatal(err)
	}
	const budget = 200 * time.Millisecond
	var slowest time.Duration
	for i := 0; i < 20; i++ {
		started := time.Now()
		item, err := f.store.Next(ctx, calllist.NextClaim{WorkspaceID: f.workspace, ListID: f.listID, UserID: uuid.NewString(), Now: now})
		elapsed := time.Since(started)
		if err != nil || item == nil {
			t.Fatalf("next %d = %+v, %v", i, item, err)
		}
		if elapsed > slowest {
			slowest = elapsed
		}
	}
	t.Logf("slowest Next over %d items: %v", calllist.MaxItems, slowest)
	if slowest > budget {
		t.Fatalf("Next took %v on a list at its cap, budget %v", slowest, budget)
	}
	due, ahead := now.Add(-time.Hour), now.Add(24*time.Hour)
	pending := []struct {
		name    string
		after   int
		afterAt *time.Time
		check   func(calllist.ItemView) bool
	}{
		{"the first pending page, due callbacks first", 0, nil, func(v calllist.ItemView) bool { return v.CallbackAt != nil && !v.CallbackAt.After(now) }},
		{"a page inside the due callbacks", 60_000, &due, func(v calllist.ItemView) bool { return v.CallbackAt != nil && v.Position > 60_000 }},
		{"a page deep in the queue", 80_000, nil, func(v calllist.ItemView) bool { return v.CallbackAt == nil && v.Position > 80_000 }},
		{"a page inside the waiting callbacks", 70_000, &ahead, func(v calllist.ItemView) bool { return v.CallbackAt != nil && v.CallbackAt.After(now) }},
	}
	for _, tc := range pending {
		started := time.Now()
		page, err := f.store.Items(ctx, calllist.ItemQuery{WorkspaceID: f.workspace, ListID: f.listID, State: calllist.StatePending,
			AsOf: now, AfterPosition: tc.after, AfterAt: tc.afterAt, Limit: calllist.MaxItemPage})
		elapsed := time.Since(started)
		if err != nil || len(page.Items) == 0 || !tc.check(page.Items[0]) {
			t.Fatalf("%s = %d items, %v", tc.name, len(page.Items), err)
		}
		placed, called := 0, 0
		for _, v := range page.Items {
			if v.LeadDistrict != "" && v.LeadName != "" {
				placed++
			}
			if v.StampedCall != nil && v.Attempts > 0 {
				called++
			}
		}
		if placed == 0 || called == 0 {
			t.Fatalf("%s read no lead place (%d) or no call (%d)", tc.name, placed, called)
		}
		t.Logf("%s: %d items in %v", tc.name, len(page.Items), elapsed)
		if elapsed > 300*time.Millisecond {
			t.Fatalf("%s took %v", tc.name, elapsed)
		}
	}
	started := time.Now()
	first, err := f.store.Items(ctx, calllist.ItemQuery{WorkspaceID: f.workspace, ListID: f.listID, Limit: calllist.MaxItemPage})
	elapsed := time.Since(started)
	if err != nil || len(first.Items) != calllist.MaxItemPage || first.Items[0].Position != 1 {
		t.Fatalf("first page = %d items, %v", len(first.Items), err)
	}
	t.Logf("the first page of every state: %v", elapsed)
	if elapsed > 300*time.Millisecond {
		t.Fatalf("the first page of every state took %v", elapsed)
	}
}

func TestBuildingAListAtItsCapFitsItsBudgetAgainstPostgres(t *testing.T) {
	f := newFixture(t, 1)
	ctx := context.Background()
	if err := f.db.Exec("UPDATE call_lists SET status = 'building' WHERE id = ?", f.listID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ClaimBuild(ctx, f.workspace, f.listID, "volume", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	const page = 1000
	for built := 0; built < calllist.MaxItems; built += page {
		items := make([]calllist.Item, 0, page)
		cursor := ""
		for i := 0; i < page; i++ {
			cursor = uuid.NewString()
			items = append(items, calllist.Item{LeadID: cursor, Phone: "5511987654321"})
		}
		if err := f.store.AppendItems(ctx, calllist.BuildBatch{WorkspaceID: f.workspace, ListID: f.listID, Claim: "volume", Items: items,
			Skipped: calllist.Skips{calllist.SkipNoNumber: 3}, Cursor: cursor, At: now}); err != nil {
			t.Fatalf("batch at %d: %v", built, err)
		}
	}
	elapsed := time.Since(started)
	l, err := f.store.Get(ctx, f.workspace, f.listID)
	if err != nil || l.ItemCount != calllist.MaxItems+1 || l.Skipped[calllist.SkipNoNumber] != 300 {
		t.Fatalf("list = %+v, %v", l, err)
	}
	t.Logf("appended %d items in %v", calllist.MaxItems, elapsed)
	if elapsed > 30*time.Second {
		t.Fatalf("building took %v", elapsed)
	}
}
