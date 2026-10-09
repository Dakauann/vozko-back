package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func TestTheSnapshotSweepKeepsTheSetsOfLiveRunsAndExportsAgainstPostgres(t *testing.T) {
	db := selectionDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws := uuid.NewString()
	ids := seedLeads(t, db, ws, 20)
	live, orphan, export, finished, building := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, snapshot := range []string{live, orphan, export, finished, building} {
		if _, err := repo.FreezeSelection(ctx, lead.SelectionQuery{WorkspaceID: ws, IDs: ids[:5]}, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	insertRun := func(id, status string) {
		err := db.Exec(`INSERT INTO lead_action_runs (id, workspace_id, actor_id, action, params, idempotency_key, status, result, created_at, updated_at)
			VALUES (?, ?, ?, 'block', '{}', ?, ?, '{}', now(), now())`, id, ws, uuid.NewString(), id, status).Error
		if err != nil {
			t.Fatal(err)
		}
	}
	insertRun(live, "queued")
	insertRun(finished, "done")
	if err := db.Exec(`INSERT INTO report_jobs (id, workspace_id, kind, format, params, status, created_at, updated_at)
		VALUES (?, ?, 'leads', 'csv', jsonb_build_object('snapshotId', ?::text), 'running', now(), now())`, uuid.NewString(), ws, export).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO call_lists (id, workspace_id, name, created_by, status, phone_source, created_at, updated_at)
		VALUES (?, ?, 'Lista', ?, 'building', 'identity', now(), now())`, building, ws, uuid.NewString()).Error; err != nil {
		t.Fatal(err)
	}

	swept, err := repo.DropSnapshotsBefore(ctx, time.Now().Add(48*time.Hour), 100)
	if err != nil || swept != 10 {
		t.Fatalf("sweep = %d, %v", swept, err)
	}
	for snapshot, want := range map[string]int{live: 5, export: 5, building: 5, orphan: 0, finished: 0} {
		if size, err := repo.SnapshotSize(ctx, ws, snapshot); err != nil || size != want {
			t.Fatalf("snapshot %s holds %d, want %d (%v)", snapshot, size, want, err)
		}
	}
}

func TestAQuantityFreezeSkipsLeadsThatAlreadyHoldTheValueAgainstPostgres(t *testing.T) {
	db := selectionDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws := uuid.NewString()
	seedLeads(t, db, ws, 60)
	byName := []shared.Sort{{Field: string(lead.SortName), Direction: shared.SortAsc}}
	cases := []struct {
		name    string
		pending *lead.Assignment
		unset   string
	}{
		{"block", &lead.Assignment{Kind: lead.AssignBlocked, Value: true}, "SELECT COUNT(*) FROM lead_selection_snapshots s JOIN leads l ON l.id = s.lead_id WHERE s.snapshot_id = ? AND l.blocked"},
		{"classify", &lead.Assignment{Kind: lead.AssignCustomField, Key: "interesse", Value: "alto"}, "SELECT COUNT(*) FROM lead_selection_snapshots s JOIN leads l ON l.id = s.lead_id WHERE s.snapshot_id = ? AND l.custom_fields -> 'interesse' = '\"alto\"'::jsonb"},
	}
	if err := db.Exec(`UPDATE leads SET custom_fields = '{"interesse":"alto"}'::jsonb WHERE workspace_id = ? AND name <= 'Lead 00010'`, ws).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := uuid.NewString()
			frozen, err := repo.FreezeSelection(ctx, lead.SelectionQuery{WorkspaceID: ws, Order: byName, Limit: 20, Pending: tc.pending}, snapshot)
			if err != nil || frozen.Size != 20 {
				t.Fatalf("freeze = %+v, %v", frozen, err)
			}
			var holding int64
			db.Raw(tc.unset, snapshot).Scan(&holding)
			if holding != 0 {
				t.Fatalf("%d leads that already hold the value count toward the limit", holding)
			}
		})
	}
}

func TestTheListTotalAndTheSelectionCountAgreeOnASearchAgainstPostgres(t *testing.T) {
	db := selectionDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws := uuid.NewString()
	ids := seedLeads(t, db, ws, 40)
	if err := db.Exec("UPDATE leads SET name = 'Maria ' || name WHERE id = ANY(?::uuid[])", "{"+ids[0]+","+ids[1]+","+ids[2]+"}").Error; err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{ids[2], ids[3]} {
		if err := db.Exec(`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label, position, created_at) VALUES (?, ?, ?, ?, 'mobile', 0, now())`,
			uuid.NewString(), ws, id, "553199887766"+string(rune('0'+i))).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, term := range []string{"Maria", "55319988776"} {
		search := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{term}},
		}}}}
		page, err := repo.List(lead.ListLeadsInput{WorkspaceID: ws, Filter: search, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 10}}})
		if err != nil {
			t.Fatal(err)
		}
		count, err := repo.CountSelection(ctx, lead.SelectionQuery{WorkspaceID: ws, Filter: search})
		if err != nil {
			t.Fatal(err)
		}
		if int64(count) != page.TotalItems || count == 0 {
			t.Fatalf("%q: list total %d, selection count %d", term, page.TotalItems, count)
		}
	}
}

func TestTheSelectionStaysWithinBudgetOn188kLeadsAgainstPostgres(t *testing.T) {
	db, ws, _ := volumeDB(t)
	if err := db.AutoMigrate(&schema.LeadSelectionSnapshot{}, &schema.LeadActionRun{}, &schema.ReportJob{}); err != nil {
		t.Fatal(err)
	}
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	interest := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: "interesse", Operator: crmfilter.OpEquals, Values: []string{"alto"}}.BindKind(crmfilter.KindEnum),
	}}}}
	measure := func(name string, budget time.Duration, fn func() error) {
		t.Helper()
		started := time.Now()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		took := time.Since(started)
		t.Logf("%s took %s (budget %s)", name, took, volumeBudget(budget))
		if took > volumeBudget(budget) {
			t.Errorf("%s took %s, over its budget of %s", name, took, volumeBudget(budget))
		}
	}
	everyone := lead.SelectionQuery{WorkspaceID: ws}
	measure("count of a filter", 1500*time.Millisecond, func() error {
		_, err := repo.CountSelection(ctx, lead.SelectionQuery{WorkspaceID: ws, Filter: interest})
		return err
	})
	measure("count of everyone", 1500*time.Millisecond, func() error {
		_, err := repo.CountSelection(ctx, everyone)
		return err
	})
	measure("freeze of everyone", 20*time.Second, func() error {
		frozen, err := repo.FreezeSelection(ctx, everyone, uuid.NewString())
		if err == nil && frozen.Size != volumeLeads {
			t.Errorf("froze %d leads, want %d", frozen.Size, volumeLeads)
		}
		return err
	})
	byActivity := lead.SelectionQuery{WorkspaceID: ws, Order: []shared.Sort{{Field: string(lead.SortLastActivityAt), Direction: shared.SortDesc}}, Limit: 150000,
		Pending: &lead.Assignment{Kind: lead.AssignBlocked, Value: true}}
	measure("freeze of the first 150,000 by last activity", 45*time.Second, func() error {
		_, err := repo.FreezeSelection(ctx, byActivity, uuid.NewString())
		return err
	})
	var batch []string
	if err := db.Raw("SELECT id::text FROM leads WHERE workspace_id = ? ORDER BY id LIMIT 500", ws).Scan(&batch).Error; err != nil {
		t.Fatal(err)
	}
	edit := leadaction.Edit{Kind: leadaction.EditCustomField, Field: lead.CustomFieldName("interesse"), Key: "interesse", Value: "baixo", Recorded: true, EventKind: lead.EventUpdated}
	measure("a 500-lead bulk write", time.Second, func() error {
		_, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: uuid.NewString(), LeadIDs: batch, Edit: edit, At: time.Now()})
		return err
	})
	s, err := repo.selection(byActivity)
	if err != nil {
		t.Fatal(err)
	}
	head, args := s.chosen()
	took, plan := explain(t, db, head, args...)
	t.Logf("first n head took %s:\n%s", took, plan)
}
