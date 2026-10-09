package calllist_repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/calls/calllist"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

type fixture struct {
	db        *gorm.DB
	store     *Store
	workspace string
	listID    string
	leads     []string
}

func newFixture(t *testing.T, leads int) *fixture {
	t.Helper()
	db := repotest.IsolatedDB(t, "call_lists")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.LeadAddress{}, &schema.Call{}, &schema.CallList{}, &schema.CallListItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return database.CreateCallListConstraints(tx) }); err != nil {
		t.Fatalf("constraints: %v", err)
	}
	f := &fixture{db: db, store: NewStore(db), workspace: uuid.NewString(), listID: uuid.NewString()}
	ctx := context.Background()
	draft := calllist.Draft{ID: f.listID, WorkspaceID: f.workspace, Name: "Retorno", CreatedBy: uuid.NewString(),
		AssigneeIDs: []string{uuid.NewString()}, Phone: calllist.PhoneChoice{Source: calllist.PhoneIdentity}}
	l, err := calllist.NewList(draft, leads, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, existed, err := f.store.Create(ctx, l); err != nil || existed {
		t.Fatalf("create = %v existed %v", err, existed)
	}
	if _, existed, err := f.store.Create(ctx, l); err != nil || !existed {
		t.Fatalf("creating the same list again = %v existed %v", err, existed)
	}
	if _, err := f.store.ClaimBuild(ctx, f.workspace, f.listID, "claim-1", now); err != nil {
		t.Fatalf("claim build: %v", err)
	}
	if _, err := f.store.ClaimBuild(ctx, f.workspace, f.listID, "claim-2", now); !errors.Is(err, calllist.ErrBuildClaimLost) {
		t.Fatalf("a second builder while the first is alive = %v", err)
	}
	items := make([]calllist.Item, 0, leads)
	for i := 0; i < leads; i++ {
		id := uuid.NewString()
		f.leads = append(f.leads, id)
		if err := db.Exec("INSERT INTO leads (id, workspace_id, number, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			id, f.workspace, fmt.Sprintf("551198765%04d", i), "Pessoa", now, now).Error; err != nil {
			t.Fatal(err)
		}
		items = append(items, calllist.Item{LeadID: id, Phone: "5511987654321"})
	}
	if err := f.store.AppendItems(ctx, calllist.BuildBatch{WorkspaceID: f.workspace, ListID: f.listID, Claim: "claim-1", Items: items,
		Skipped: calllist.Skips{calllist.SkipGone: 1}, Cursor: f.leads[len(f.leads)-1], At: now}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := f.store.AppendItems(ctx, calllist.BuildBatch{WorkspaceID: f.workspace, ListID: f.listID, Claim: "claim-1", Items: items[:1], At: now}); err != nil {
		t.Fatalf("appending a lead again: %v", err)
	}
	if err := f.store.FinishBuild(ctx, f.workspace, f.listID, "claim-2", calllist.StatusActive, "", now); !errors.Is(err, calllist.ErrBuildClaimLost) {
		t.Fatalf("finishing with a stale claim = %v", err)
	}
	if err := f.store.FinishBuild(ctx, f.workspace, f.listID, "claim-1", calllist.StatusActive, "", now); err != nil {
		t.Fatalf("finish: %v", err)
	}
	built, err := f.store.Get(ctx, f.workspace, f.listID)
	if err != nil {
		t.Fatal(err)
	}
	if built.Status != calllist.StatusActive || built.ItemCount != leads || built.Skipped[calllist.SkipGone] != 1 || built.Build.Claim != "" {
		t.Fatalf("built list = %+v", built)
	}
	return f
}

func (f *fixture) itemOfLead(t *testing.T, leadID string) *calllist.Item {
	t.Helper()
	var id string
	f.db.Raw("SELECT id::text FROM call_list_items WHERE list_id = ? AND lead_id = ?", f.listID, leadID).Scan(&id)
	item, err := f.store.Item(context.Background(), f.workspace, id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) next(t *testing.T, user string, at time.Time) *calllist.Item {
	t.Helper()
	item, err := f.store.Next(context.Background(), calllist.NextClaim{WorkspaceID: f.workspace, ListID: f.listID, UserID: user, Now: at})
	if err != nil {
		t.Fatalf("next for %s: %v", user, err)
	}
	return item
}

func TestTheQueueServesDueCallbacksFirstThenExpiredItemsThenPositionAgainstPostgres(t *testing.T) {
	f := newFixture(t, 4)
	due := now.Add(-time.Minute)
	later := now.Add(time.Hour)
	f.db.Exec("UPDATE call_list_items SET callback_at = ? WHERE lead_id = ?", due, f.leads[3])
	f.db.Exec("UPDATE call_list_items SET callback_at = ? WHERE lead_id = ?", later, f.leads[0])
	stale := now.Add(-time.Second)
	f.db.Exec("UPDATE call_list_items SET state = 'reserved', reserved_by = ?, reserved_until = ? WHERE lead_id = ?", uuid.NewString(), stale, f.leads[2])

	order := []string{}
	for i := 0; i < 3; i++ {
		item := f.next(t, uuid.NewString(), now)
		if item == nil {
			t.Fatalf("worker %d found nothing", i)
		}
		order = append(order, item.LeadID)
	}
	want := []string{f.leads[3], f.leads[2], f.leads[1]}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("claim order = %v, want %v", order, want)
		}
	}
	if item := f.next(t, uuid.NewString(), now); item != nil {
		t.Fatalf("a callback for later must wait, got %+v", item)
	}
	if item := f.next(t, uuid.NewString(), later); item == nil || item.LeadID != f.leads[0] {
		t.Fatalf("the callback once due = %+v", item)
	}
}

func TestAWorkerHoldsOneItemAndGetsItBackUntilItExpiresAgainstPostgres(t *testing.T) {
	f := newFixture(t, 3)
	user := uuid.NewString()
	first := f.next(t, user, now)
	again := f.next(t, user, now.Add(time.Minute))
	if again.ID != first.ID {
		t.Fatalf("asking again while holding an item gave %s, want %s", again.ID, first.ID)
	}
	if !again.ReservedUntil.Equal(now.Add(time.Minute + calllist.ReservationTTL)) {
		t.Fatalf("reserved until %v", again.ReservedUntil)
	}

	afterExpiry := now.Add(time.Minute + calllist.ReservationTTL + time.Second)
	other := f.next(t, uuid.NewString(), afterExpiry)
	if other == nil || other.ID != first.ID {
		t.Fatalf("the expired item must return to the queue for someone else, got %+v", other)
	}
	moved := f.next(t, user, afterExpiry)
	if moved == nil || moved.ID == first.ID {
		t.Fatalf("the worker whose reservation expired must get the next free item, got %+v", moved)
	}
	var reserved int64
	f.db.Raw("SELECT COUNT(*) FROM call_list_items WHERE reserved_by = ? AND state = 'reserved'", user).Scan(&reserved)
	if reserved != 1 {
		t.Fatalf("the worker holds %d items, want 1", reserved)
	}
}

func TestTheWorkersOwnExpiredReservationIsReleasedBeforeTheNextClaimAgainstPostgres(t *testing.T) {
	f := newFixture(t, 2)
	user := uuid.NewString()
	first := f.next(t, user, now)
	f.db.Exec("UPDATE call_list_items SET position = 99 WHERE id = ?", first.ID)
	afterExpiry := now.Add(calllist.ReservationTTL + time.Second)
	second := f.next(t, user, afterExpiry)
	if second == nil || second.ID == first.ID {
		t.Fatalf("next after expiry = %+v", second)
	}
	released, err := f.store.Item(context.Background(), f.workspace, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != calllist.StatePending || released.ReservedBy != "" {
		t.Fatalf("the expired item = %+v, want it back in the queue", released)
	}
}

func TestConcurrentNextCallsNeverHandOneItemToTwoWorkersAgainstPostgres(t *testing.T) {
	f := newFixture(t, 6)
	var wg sync.WaitGroup
	got := make([]string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			item, err := f.store.Next(context.Background(), calllist.NextClaim{WorkspaceID: f.workspace, ListID: f.listID, UserID: uuid.NewString(), Now: now})
			if err == nil && item != nil {
				got[i] = item.ID
			}
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, id := range got {
		if id == "" || seen[id] {
			t.Fatalf("claims = %v", got)
		}
		seen[id] = true
	}
}

func TestClosingAnItemCountsItOnceOnTheListAgainstPostgres(t *testing.T) {
	f := newFixture(t, 2)
	ctx := context.Background()
	user := uuid.NewString()
	item := f.next(t, user, now)
	callID := uuid.NewString()
	if err := f.db.Exec("INSERT INTO calls (id, call_id, workspace_id, type, direction, source, status, agent_id, lead_id, started_at, created_at, updated_at)"+
		" VALUES (?, ?, ?, 'crm', 'outbound', 'sip_trunk', 'completed', ?, ?, ?, ?, ?)",
		callID, "sip-"+callID, f.workspace, user, item.LeadID, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Mutate(ctx, f.workspace, item.ID, func(i *calllist.Item, _ *calllist.List) error {
		return i.Stamp(user, callID, now)
	}); err != nil {
		t.Fatal(err)
	}
	closed, err := f.store.Mutate(ctx, f.workspace, item.ID, func(i *calllist.Item, l *calllist.List) error {
		if l.ID != f.listID {
			t.Fatalf("list = %s", l.ID)
		}
		_, err := i.Refuse(calllist.SkipGone, now)
		return err
	})
	if err != nil || closed.State != calllist.StateClosed {
		t.Fatalf("mutate = %+v, %v", closed, err)
	}
	refused := errors.New("refused")
	if _, err := f.store.Mutate(ctx, f.workspace, item.ID, func(*calllist.Item, *calllist.List) error { return refused }); !errors.Is(err, refused) {
		t.Fatalf("a refused mutation = %v", err)
	}
	l, err := f.store.Get(ctx, f.workspace, f.listID)
	if err != nil || l.ClosedCount != 1 || l.CalledCount != 1 || l.CallbackCount != 0 {
		t.Fatalf("progress = closed %d called %d callbacks %d, %v", l.ClosedCount, l.CalledCount, l.CallbackCount, err)
	}

	page, err := f.store.Items(ctx, calllist.ItemQuery{WorkspaceID: f.workspace, ListID: f.listID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Next == 0 {
		t.Fatalf("first page = %+v", page)
	}
	rest, err := f.store.Items(ctx, calllist.ItemQuery{WorkspaceID: f.workspace, ListID: f.listID, AfterPosition: page.Next})
	if err != nil || len(rest.Items) != 1 || rest.Next != 0 {
		t.Fatalf("second page = %+v, %v", rest, err)
	}
	closedPage, err := f.store.Items(ctx, calllist.ItemQuery{WorkspaceID: f.workspace, ListID: f.listID, State: calllist.StateClosed})
	if err != nil || len(closedPage.Items) != 1 {
		t.Fatalf("closed items = %+v, %v", closedPage, err)
	}
	view := closedPage.Items[0]
	if view.LastCall == nil || view.LastCall.ID != callID || view.Attempts != 1 || view.LeadName != "Pessoa" ||
		view.StampedCall == nil || *view.StampedCall != (calllist.CallFacts{ID: callID, WorkspaceID: f.workspace, LeadID: item.LeadID, AgentID: user}) {
		t.Fatalf("view = %+v call %+v", view, view.LastCall)
	}
}

func TestDeletingAListTakesItsItemsAgainstPostgres(t *testing.T) {
	f := newFixture(t, 2)
	l, err := f.store.Get(context.Background(), f.workspace, f.listID)
	if err != nil {
		t.Fatal(err)
	}
	name, paused, members := "Novo nome", calllist.StatusPaused, []string{uuid.NewString(), uuid.NewString()}
	if err := l.Change(calllist.Change{Name: &name, Status: &paused, AssigneeIDs: &members}, now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Update(context.Background(), l); err != nil {
		t.Fatalf("update: %v", err)
	}
	changed, err := f.store.Get(context.Background(), f.workspace, f.listID)
	if err != nil || changed.Name != "Novo nome" || changed.Status != calllist.StatusPaused || len(changed.AssigneeIDs) != 2 {
		t.Fatalf("changed = %+v, %v", changed, err)
	}
	if err := f.store.Delete(context.Background(), f.workspace, f.listID); err != nil {
		t.Fatal(err)
	}
	var left int64
	f.db.Raw("SELECT COUNT(*) FROM call_list_items WHERE list_id = ?", f.listID).Scan(&left)
	if left != 0 {
		t.Fatalf("%d items left", left)
	}
	if err := f.store.Delete(context.Background(), f.workspace, f.listID); !errors.Is(err, calllist.ErrListNotFound) {
		t.Fatalf("deleting again = %v", err)
	}
}

func TestAStalledBuildIsResumedAndAnExhaustedOneFailsAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "call_list_builds")
	if err := db.AutoMigrate(&schema.CallList{}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	ctx := context.Background()
	ws := uuid.NewString()
	insert := func(attempts int, heartbeat *time.Time) string {
		id := uuid.NewString()
		if err := db.Exec("INSERT INTO call_lists (id, workspace_id, name, created_by, status, phone_source, attempts, claim_token, heartbeat_at, created_at, updated_at)"+
			" VALUES (?, ?, 'L', ?, 'building', 'identity', ?, 'old', ?, ?, ?)", id, ws, uuid.NewString(), attempts, heartbeat, now, now).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	stale := now.Add(-calllist.BuildLeaseStale - time.Second)
	alive := now.Add(-time.Second)
	resumable := insert(1, &stale)
	insert(1, &alive)
	exhausted := insert(calllist.MaxBuildAttempts, &stale)

	refs, err := store.Buildable(ctx, now, 10)
	if err != nil || len(refs) != 1 || refs[0].ID != resumable || refs[0].WorkspaceID != ws {
		t.Fatalf("buildable = %+v, %v", refs, err)
	}
	failed, err := store.FailExhausted(ctx, now)
	if err != nil || failed != 1 {
		t.Fatalf("failed = %d, %v", failed, err)
	}
	l, err := store.Get(ctx, ws, exhausted)
	if err != nil || l.Status != calllist.StatusFailed || l.FailureCode != calllist.FailureBuild {
		t.Fatalf("exhausted list = %+v, %v", l, err)
	}
}

func TestAppendedItemsTakeTheNextFreePositionsEvenAfterAConflictAgainstPostgres(t *testing.T) {
	f := newFixture(t, 3)
	ctx := context.Background()
	if err := f.db.Exec("UPDATE call_lists SET status = 'building' WHERE id = ?", f.listID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ClaimBuild(ctx, f.workspace, f.listID, "again", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	fresh, later := uuid.NewString(), uuid.NewString()
	batch := []calllist.Item{{LeadID: f.leads[0], Phone: "5511987654321"}, {LeadID: fresh, Phone: "5511987654321"}, {LeadID: fresh, Phone: "5511987654322"}}
	if err := f.store.AppendItems(ctx, calllist.BuildBatch{WorkspaceID: f.workspace, ListID: f.listID, Claim: "again", Items: batch, At: now}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.AppendItems(ctx, calllist.BuildBatch{WorkspaceID: f.workspace, ListID: f.listID, Claim: "again",
		Items: []calllist.Item{{LeadID: later, Phone: "5511987654321"}}, At: now}); err != nil {
		t.Fatal(err)
	}
	var positions []int
	f.db.Raw("SELECT position FROM call_list_items WHERE list_id = ? ORDER BY position", f.listID).Scan(&positions)
	if fmt.Sprint(positions) != "[1 2 3 4 5]" {
		t.Fatalf("positions = %v, want one free position per written row", positions)
	}
	if f.itemOfLead(t, fresh).Position != 4 || f.itemOfLead(t, fresh).Phone != "5511987654321" || f.itemOfLead(t, later).Position != 5 {
		t.Fatalf("fresh %+v, later %+v", f.itemOfLead(t, fresh), f.itemOfLead(t, later))
	}
	l, err := f.store.Get(ctx, f.workspace, f.listID)
	if err != nil || l.ItemCount != 5 {
		t.Fatalf("list = %+v, %v", l, err)
	}
	if err := f.db.Exec("INSERT INTO call_list_items (id, workspace_id, list_id, lead_id, phone, position, state, created_at, updated_at) VALUES (?, ?, ?, ?, '5511987654321', 5, 'pending', ?, ?)",
		uuid.NewString(), f.workspace, f.listID, uuid.NewString(), now, now).Error; err == nil {
		t.Fatal("the database accepted two items at the same position")
	}
}

func (f *fixture) leadsOf(page calllist.ItemPage) []string {
	out := []string{}
	for _, view := range page.Items {
		out = append(out, view.LeadID)
	}
	return out
}

func (f *fixture) pending(t *testing.T, asOf time.Time, after int, afterAt *time.Time, limit int) calllist.ItemPage {
	t.Helper()
	page, err := f.store.Items(context.Background(), calllist.ItemQuery{WorkspaceID: f.workspace, ListID: f.listID, State: calllist.StatePending,
		AsOf: asOf, AfterPosition: after, AfterAt: afterAt, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func (f *fixture) agendaFixture(t *testing.T) {
	t.Helper()
	for _, step := range []struct {
		sql  string
		args []interface{}
	}{
		{"UPDATE call_list_items SET callback_at = ? WHERE lead_id = ?", []interface{}{now.Add(time.Hour), f.leads[1]}},
		{"UPDATE call_list_items SET callback_at = ? WHERE lead_id = ?", []interface{}{now.Add(-time.Minute), f.leads[3]}},
		{"UPDATE call_list_items SET state = 'closed' WHERE lead_id = ?", []interface{}{f.leads[4]}},
		{"UPDATE call_list_items SET callback_at = ? WHERE lead_id = ?", []interface{}{now.Add(-2 * time.Hour), f.leads[5]}},
		{"UPDATE call_list_items SET callback_at = ?, refusal = 'unknown_purpose' WHERE lead_id = ?", []interface{}{now.Add(24 * time.Hour), f.leads[6]}},
	} {
		if err := f.db.Exec(step.sql, step.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestThePendingTabShowsDueCallbacksThenTheQueueThenWaitingCallbacksWithTheBairroAgainstPostgres(t *testing.T) {
	f := newFixture(t, 7)
	f.agendaFixture(t)
	if err := f.db.Exec("INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, district, city, geo_status, fingerprint, created_at, updated_at)"+
		" VALUES (?, ?, ?, 'home', true, 'Aldeia', 'Barueri', 'pending', 'f1', ?, ?), (?, ?, ?, 'work', false, 'Centro', 'Osasco', 'pending', 'f2', ?, ?)",
		uuid.NewString(), f.workspace, f.leads[0], now, now, uuid.NewString(), f.workspace, f.leads[0], now, now).Error; err != nil {
		t.Fatal(err)
	}

	whole := f.pending(t, now, 0, nil, 50)
	want := []string{f.leads[5], f.leads[3], f.leads[0], f.leads[2], f.leads[1], f.leads[6]}
	if got := fmt.Sprint(f.leadsOf(whole)); got != fmt.Sprint(want) || whole.Next != 0 || whole.AsOf == nil || !whole.AsOf.Equal(now) {
		t.Fatalf("pending tab = %s next %d as of %v, want %v", got, whole.Next, whole.AsOf, want)
	}
	if whole.Items[2].LeadDistrict != "Aldeia" || whole.Items[2].LeadCity != "Barueri" || whole.Items[3].LeadDistrict != "" {
		t.Fatalf("bairros = %+v", whole.Items)
	}

	var pages [][]string
	after, afterAt := 0, (*time.Time)(nil)
	for i := 0; i < 10; i++ {
		page := f.pending(t, now, after, afterAt, 2)
		pages = append(pages, f.leadsOf(page))
		if page.Next == 0 {
			break
		}
		after, afterAt = page.Next, page.NextAt
	}
	if got, wantPages := fmt.Sprint(pages), fmt.Sprint([][]string{want[0:2], want[2:4], want[4:6]}); got != wantPages {
		t.Fatalf("pages = %s, want %s", got, wantPages)
	}

	all, err := f.store.Items(context.Background(), calllist.ItemQuery{WorkspaceID: f.workspace, ListID: f.listID})
	if err != nil || len(all.Items) != 7 || all.Items[0].LeadID != f.leads[0] || all.Items[6].LeadID != f.leads[6] {
		t.Fatalf("every state stays in position order: %v, %v", f.leadsOf(all), err)
	}
}

func TestThePendingTabSkipsNothingWhenTheItemAPageEndedOnChangesAgainstPostgres(t *testing.T) {
	f := newFixture(t, 7)
	f.agendaFixture(t)
	asOf := now

	first := f.pending(t, asOf, 0, nil, 2)
	if got := fmt.Sprint(f.leadsOf(first)); got != fmt.Sprint([]string{f.leads[5], f.leads[3]}) || first.Next == 0 || first.NextAt == nil {
		t.Fatalf("first page = %s next %d at %v", got, first.Next, first.NextAt)
	}
	boundary := f.itemOfLead(t, f.leads[3])
	if err := f.db.Exec("UPDATE call_list_items SET state = 'closed', callback_at = NULL, disposition = 'interessado' WHERE id = ?", boundary.ID).Error; err != nil {
		t.Fatal(err)
	}
	second := f.pending(t, asOf, first.Next, first.NextAt, 2)
	if got := fmt.Sprint(f.leadsOf(second)); got != fmt.Sprint([]string{f.leads[0], f.leads[2]}) || second.Next == 0 || second.NextAt != nil {
		t.Fatalf("after the boundary item was closed = %s next %d at %v", got, second.Next, second.NextAt)
	}

	gone := f.itemOfLead(t, f.leads[2])
	if err := f.db.Exec("DELETE FROM call_list_items WHERE id = ?", gone.ID).Error; err != nil {
		t.Fatal(err)
	}
	third := f.pending(t, asOf, second.Next, second.NextAt, 2)
	if got := fmt.Sprint(f.leadsOf(third)); got != fmt.Sprint([]string{f.leads[1], f.leads[6]}) || third.Next != 0 {
		t.Fatalf("after the cursor row was deleted = %s next %d", got, third.Next)
	}

	if err := f.db.Exec("UPDATE call_list_items SET callback_at = ? WHERE lead_id = ?", now.Add(-time.Hour), f.leads[1]).Error; err != nil {
		t.Fatal(err)
	}
	rescheduled := f.pending(t, asOf, 0, nil, 1)
	if err := f.db.Exec("UPDATE call_list_items SET callback_at = ? WHERE lead_id = ?", now.Add(48*time.Hour), f.leads[5]).Error; err != nil {
		t.Fatal(err)
	}
	next := f.pending(t, asOf, rescheduled.Next, rescheduled.NextAt, 2)
	if got := fmt.Sprint(f.leadsOf(next)); got != fmt.Sprint([]string{f.leads[1], f.leads[0]}) {
		t.Fatalf("after the boundary callback moved two days ahead = %s, want the callbacks that were due and then the queue", got)
	}
}

func TestACallbackIsCountedUntilANewCallAgainstPostgres(t *testing.T) {
	f := newFixture(t, 1)
	ctx := context.Background()
	user := uuid.NewString()
	item := f.next(t, user, now)
	insertCall := func() string {
		id := uuid.NewString()
		if err := f.db.Exec("INSERT INTO calls (id, call_id, workspace_id, type, direction, source, status, agent_id, lead_id, started_at, created_at, updated_at)"+
			" VALUES (?, ?, ?, 'crm', 'outbound', 'sip_trunk', 'completed', ?, ?, ?, ?, ?)", id, "sip-"+id, f.workspace, user, item.LeadID, now, now, now).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := insertCall()
	stamp := func(callID string) {
		t.Helper()
		if _, err := f.store.Mutate(ctx, f.workspace, item.ID, func(i *calllist.Item, _ *calllist.List) error { return i.Stamp(user, callID, now) }); err != nil {
			t.Fatal(err)
		}
	}
	progress := func() [3]int {
		t.Helper()
		l, err := f.store.Get(ctx, f.workspace, f.listID)
		if err != nil {
			t.Fatal(err)
		}
		return [3]int{l.CalledCount, l.CallbackCount, l.ClosedCount}
	}
	stamp(first)
	callback := now.Add(time.Hour)
	if _, err := f.store.Mutate(ctx, f.workspace, item.ID, func(i *calllist.Item, _ *calllist.List) error {
		_, err := i.Close(calllist.Closing{By: user, Disposition: calllist.DispositionCallback, CallbackAt: &callback},
			&calllist.CallFacts{ID: first, WorkspaceID: f.workspace, LeadID: item.LeadID, AgentID: user}, nil, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := progress(); got != [3]int{1, 1, 0} {
		t.Fatalf("after the callback: called, callbacks, closed = %v", got)
	}
	stamp(insertCall())
	if got := progress(); got != [3]int{1, 0, 0} {
		t.Fatalf("after the new call: called, callbacks, closed = %v", got)
	}
}

func TestTheRecountRestoresTheProgressOfListsBuiltBeforeTheCountersAgainstPostgres(t *testing.T) {
	f := newFixture(t, 3)
	ctx := context.Background()
	callID := uuid.NewString()
	if err := f.db.Exec("INSERT INTO calls (id, call_id, workspace_id, type, direction, source, status, lead_id, started_at, created_at, updated_at)"+
		" VALUES (?, ?, ?, 'crm', 'outbound', 'sip_trunk', 'completed', ?, ?, ?, ?)", callID, "sip-"+callID, f.workspace, f.leads[0], now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		sql  string
		args []interface{}
	}{
		{"UPDATE call_list_items SET last_call_id = ?, disposition = '_callback', callback_at = ? WHERE lead_id = ?", []interface{}{callID, now.Add(time.Hour), f.leads[0]}},
		{"UPDATE call_list_items SET state = 'closed', disposition = '_callback' WHERE lead_id = ?", []interface{}{f.leads[1]}},
		{"UPDATE call_lists SET called_count = 0, callback_count = 0 WHERE id = ?", []interface{}{f.listID}},
	} {
		if err := f.db.Exec(step.sql, step.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	item := f.itemOfLead(t, f.leads[0])
	if _, err := f.store.Mutate(ctx, f.workspace, item.ID, func(i *calllist.Item, _ *calllist.List) error {
		i.Disposition, i.CallbackAt = "", nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if l, err := f.store.Get(ctx, f.workspace, f.listID); err != nil || l.CallbackCount != 0 {
		t.Fatalf("a callback the counters never saw went below zero: %+v, %v", l, err)
	}
	if err := f.db.Exec("UPDATE call_list_items SET disposition = '_callback', callback_at = ? WHERE id = ?", now.Add(time.Hour), item.ID).Error; err != nil {
		t.Fatal(err)
	}

	next, recounted, err := f.store.RecountProgress(ctx, "", 100)
	if err != nil || next != f.listID || recounted != 1 {
		t.Fatalf("recount = %q, %d, %v", next, recounted, err)
	}
	l, err := f.store.Get(ctx, f.workspace, f.listID)
	if err != nil || l.CalledCount != 1 || l.CallbackCount != 1 {
		t.Fatalf("recounted list = called %d, callbacks %d, %v", l.CalledCount, l.CallbackCount, err)
	}
	if _, again, err := f.store.RecountProgress(ctx, "", 100); err != nil || again != 0 {
		t.Fatalf("a second recount changed %d lists, %v", again, err)
	}
	if done, _, err := f.store.RecountProgress(ctx, next, 100); err != nil || done != "" {
		t.Fatalf("the recount after the last list = %q, %v", done, err)
	}
}
