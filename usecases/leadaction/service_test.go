package leadaction_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"sync"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/report"
	"vozko/domain/selection"
)

func interest() *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse},
	}}}}
}

func matchingAll(expected int) selection.Selection {
	f := interest()
	return selection.Selection{Mode: selection.ModeAllMatching, Filter: f, Fingerprint: selection.Fingerprint(*f), ExpectedCount: expected}
}

func classify(value string, s selection.Selection) Request {
	return Request{
		Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionClassify,
		Params: leadaction.Params{Key: "interesse", Value: json.RawMessage(value)}, Selection: s, IdempotencyKey: "key-1",
	}
}

func block(s selection.Selection, phone string) Request {
	yes := true
	return Request{
		Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionBlock,
		Params: leadaction.Params{Blocked: &yes, BusinessPhoneID: phone}, Selection: s, IdempotencyKey: "key-block",
	}
}

func TestStartQueuesARunOverTheFrozenSelection(t *testing.T) {
	h := newHarness(1200)
	out, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(1200)))
	if err != nil {
		t.Fatal(err)
	}
	run := out.Run
	if run == nil || run.Status != leadaction.StatusQueued || run.Result.Matched != 1200 || run.Result.Selected != 1200 || run.ActorID != manager {
		t.Fatalf("run = %+v", run)
	}
	if _, frozen := h.selections.snapshots[run.ID]; !frozen || h.selections.freezes != 1 {
		t.Fatal("the run does not hold its frozen selection")
	}
	if len(h.queued) != 1 || h.gate.acquired == 0 {
		t.Fatalf("the run was not launched behind the gate: %d queued, %d gate slots", len(h.queued), h.gate.acquired)
	}
}

func TestTheSameKeyReturnsTheSameRunAndFreezesOnce(t *testing.T) {
	h := newHarness(10)
	first, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10)))
	if err != nil {
		t.Fatal(err)
	}
	h.selections.matched = 11
	again, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10)))
	if err != nil {
		t.Fatalf("a retry of the same request = %v", err)
	}
	if again.Run.ID != first.Run.ID || h.selections.freezes != 1 || h.runs.create != 1 {
		t.Fatalf("a retry created %d runs and %d freezes", h.runs.create, h.selections.freezes)
	}
	if _, err := h.svc.Start(context.Background(), classify(`"baixo"`, matchingAll(10))); !errors.Is(err, leadaction.ErrIdempotencyKeyReused) {
		t.Fatalf("a key reused for another value = %v", err)
	}
}

func TestStartRefusesWithoutTheCapabilities(t *testing.T) {
	h := newHarness(10)
	sellerReq := classify(`"alto"`, matchingAll(10))
	sellerReq.Actor.UserID = seller
	if _, err := h.svc.Start(context.Background(), sellerReq); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("a seller classified a selection: %v", err)
	}
	h.perms[manager]["leads:read_sensitive"] = false
	sensitive := classify(`"Positivo"`, matchingAll(10))
	sensitive.Params.Key = "classificacao"
	if _, err := h.svc.Start(context.Background(), sensitive); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("a sensitive field was classified without read_sensitive: %v", err)
	}
	h.perms[manager]["leads:block"] = false
	if _, err := h.svc.Start(context.Background(), block(matchingAll(10), "")); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("a selection was blocked without leads:block: %v", err)
	}
	if h.selections.freezes != 0 || h.runs.create != 0 {
		t.Fatal("a refused action froze or stored something")
	}
}

func TestStartRefusesWhatItCannotCheck(t *testing.T) {
	h := newHarness(10)
	noKey := classify(`"alto"`, matchingAll(10))
	noKey.IdempotencyKey = ""
	if _, err := h.svc.Start(context.Background(), noKey); !errors.Is(err, leadaction.ErrIdempotencyKeyRequired) {
		t.Fatalf("a request without an Idempotency-Key = %v", err)
	}
	unconfirmed := classify(`"alto"`, selection.Selection{Mode: selection.ModeAllMatching, Filter: interest()})
	if _, err := h.svc.Start(context.Background(), unconfirmed); !errors.Is(err, selection.ErrFingerprintMismatch) {
		t.Fatalf("an unconfirmed selection = %v", err)
	}
	wrong := classify(`"talvez"`, matchingAll(10))
	if _, err := h.svc.Start(context.Background(), wrong); err == nil {
		t.Fatal("a value outside the options was accepted")
	}
	h.owners.refused["ai:robot"] = lead.ErrLeadOwnerOutsideWorkspace
	owner := "ai:robot"
	assign := Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionAssignOwner,
		Params: leadaction.Params{OwnerID: &owner}, Selection: matchingAll(10), IdempotencyKey: "k-assign"}
	if _, err := h.svc.Start(context.Background(), assign); !errors.Is(err, lead.ErrLeadOwnerOutsideWorkspace) {
		t.Fatalf("an owner outside the workspace = %v", err)
	}
}

func TestStartConfirmsTheCountAndRefusesAnEmptySelection(t *testing.T) {
	h := newHarness(10)
	h.selections.matched = 12
	var changed *selection.CountChangedError
	if _, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10))); !errors.As(err, &changed) || changed.Matched != 12 {
		t.Fatalf("a count that moved = %v", err)
	}
	empty := newHarness(0)
	empty.selections.matched = 3
	if _, err := empty.svc.Start(context.Background(), classify(`"alto"`, matchingAll(3))); !errors.Is(err, leadaction.ErrSelectionEmpty) {
		t.Fatalf("an empty frozen set = %v", err)
	}
	if h.runs.create != 0 || empty.runs.create != 0 {
		t.Fatal("a refused selection became a run")
	}
}

func TestProcessAppliesBatchesAndFinishes(t *testing.T) {
	h := newHarness(1200)
	out, _ := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(1200)))
	h.drain()

	run := h.runs.only()
	if run.Status != leadaction.StatusDone || run.Result.Processed != 1200 || run.Result.Changed != 1200 {
		t.Fatalf("finished run = %+v", run)
	}
	if !reflect.DeepEqual(h.writer.sizes, []int{500, 500, 200}) || len(h.notifier.runs) != 3 || h.notifier.runs[0] != out.Run.ID {
		t.Fatalf("batches = %v, notified %v", h.writer.sizes, h.notifier.runs)
	}
	if !reflect.DeepEqual(h.selections.dropped, []string{out.Run.ID}) {
		t.Fatalf("the frozen set was not dropped: %v", h.selections.dropped)
	}
}

func TestTheSameRunDeliveredTwiceChangesNothingNew(t *testing.T) {
	h := newHarness(600)
	out, _ := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(600)))
	h.drain()
	events := h.writer.events
	if err := h.svc.Process(context.Background(), out.Run.ID); err != nil {
		t.Fatal(err)
	}
	if h.writer.events != events || h.runs.only().Result.Changed != 600 {
		t.Fatalf("a second delivery wrote %d new events", h.writer.events-events)
	}

	second := newHarness(600)
	first, _ := second.svc.Start(context.Background(), classify(`"alto"`, matchingAll(600)))
	second.drain()
	retry := classify(`"alto"`, matchingAll(600))
	retry.IdempotencyKey = "key-2"
	again, err := second.svc.Start(context.Background(), retry)
	if err != nil {
		t.Fatal(err)
	}
	second.drain()
	done, _ := second.runs.Get(context.Background(), workspaceID, again.Run.ID)
	if done.Result.Changed != 0 || done.Result.Skipped[leadaction.SkipUnchanged] != 600 || first.Run.ID == again.Run.ID {
		t.Fatalf("a repeated classification changed %d leads", done.Result.Changed)
	}
}

func TestProcessChecksThePermissionsAgain(t *testing.T) {
	h := newHarness(10)
	if _, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10))); err != nil {
		t.Fatal(err)
	}
	h.perms[manager]["leads:bulk_update"] = false
	h.drain()
	run := h.runs.only()
	if run.Status != leadaction.StatusFailed || run.FailureCode != leadaction.FailureForbidden || h.writer.batches != 0 {
		t.Fatalf("a run of an actor who lost the permission = %+v, %d batches", run, h.writer.batches)
	}
}

func TestBlockAppliesTheMetaBlockUnderTheLimiter(t *testing.T) {
	h := newHarness(5)
	h.limiter.allow = 2
	if _, err := h.svc.Start(context.Background(), block(matchingAll(5), phoneID)); err != nil {
		t.Fatal(err)
	}
	h.drain()
	run := h.runs.only()
	if run.Status != leadaction.StatusQueued || run.Phase != leadaction.PhaseMeta || run.NotBefore == nil || run.Result.MetaApplied != 2 {
		t.Fatalf("a run over the Meta limit = %+v", run)
	}
	if len(h.phone.applied) != 2 || h.limiter.keys[0] != phoneID {
		t.Fatalf("applied %v with keys %v", h.phone.applied, h.limiter.keys)
	}

	h.limiter.allow = 10
	if err := h.svc.Process(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if again := h.runs.only(); again.Status != leadaction.StatusQueued {
		t.Fatalf("a deferred run was claimed before its time: %+v", again)
	}
	if len(h.scheduled) != 1 || h.scheduled[0].after != 30*time.Second {
		t.Fatalf("the deferred run was not scheduled again: %+v", h.scheduled)
	}
	h.clock.at = *run.NotBefore
	h.scheduled[0].run()
	h.drain()
	done := h.runs.only()
	if done.Status != leadaction.StatusDone || done.Result.MetaApplied != 5 || len(h.phone.applied) != 5 {
		t.Fatalf("finished block run = %+v, applied %v", done, h.phone.applied)
	}
}

func TestSweepFailsStalledRunsAndRelaunchesTheRest(t *testing.T) {
	h := newHarness(10)
	out, _ := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10)))
	h.queued = nil
	stuck, _ := h.runs.Claim(context.Background(), out.Run.ID, "dead-worker", start)
	if stuck == nil {
		t.Fatal("claim failed")
	}
	h.clock.at = start.Add(leadaction.StaleAfter + time.Second)
	if err := h.svc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if run := h.runs.only(); run.Status != leadaction.StatusDone {
		t.Fatalf("a run left behind by a dead worker was not resumed: %+v", run)
	}
}

func TestPreviewCountsSkipsInChunks(t *testing.T) {
	h := newHarness(7000)
	for _, id := range leadIDs(7000)[:30] {
		h.writer.leads[id].blocked = true
	}
	p, err := h.svc.Preview(context.Background(), block(selection.Selection{Mode: selection.ModeAllMatching, Filter: interest()}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != leadaction.PreviewDone || p.Result.Matched != 7000 || p.Result.Selected != 7000 || p.Result.Eligible != 6970 ||
		p.Result.Skipped[leadaction.SkipUnchanged] != 30 || p.Result.ExpectedCount != 7000 || p.Result.Fingerprint != selection.Fingerprint(*interest()) {
		t.Fatalf("preview = %+v", p)
	}
	if !reflect.DeepEqual(h.selections.resolved, []string{"", leadIDs(7000)[4999]}) {
		t.Fatalf("pages read after %v", h.selections.resolved)
	}
	if h.selections.freezes != 0 || h.writer.batches != 0 {
		t.Fatal("a preview froze or wrote something")
	}
}

func TestAPreviewOverItsBudgetFinishesInTheBackground(t *testing.T) {
	h := newHarness(7000)
	h.svc.deps.Now = func() time.Time {
		h.clock.at = h.clock.at.Add(4 * time.Second)
		return h.clock.at
	}
	p, err := h.svc.Preview(context.Background(), block(selection.Selection{Mode: selection.ModeAllMatching, Filter: interest()}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != leadaction.PreviewRunning || p.ID == "" || len(h.queued) != 1 {
		t.Fatalf("a slow preview = %+v, %d queued", p, len(h.queued))
	}
	h.drain()
	done, err := h.svc.PreviewStatus(context.Background(), Actor{WorkspaceID: workspaceID, UserID: manager}, p.ID)
	if err != nil || done.Status != leadaction.PreviewDone || done.Result.Selected != 7000 {
		t.Fatalf("the finished preview = %+v, %v", done, err)
	}
	if _, err := h.svc.PreviewStatus(context.Background(), Actor{WorkspaceID: workspaceID, UserID: seller}, p.ID); !errors.Is(err, leadaction.ErrPreviewNotFound) {
		t.Fatalf("another member read the preview: %v", err)
	}
}

func TestTheBackgroundTallyNeverChangesThePreviewItReturned(t *testing.T) {
	h := newHarness(12000)
	h.svc.deps.Now = func() time.Time {
		h.clock.at = h.clock.at.Add(4 * time.Second)
		return h.clock.at
	}
	p, err := h.svc.Preview(context.Background(), block(selection.Selection{Mode: selection.ModeAllMatching, Filter: interest()}, ""))
	if err != nil {
		t.Fatal(err)
	}
	returned := *p
	returned.Result.Skipped = maps.Clone(p.Result.Skipped)
	h.drain()
	if p.Status != returned.Status || p.Result.Selected != returned.Result.Selected || p.Result.Eligible != returned.Result.Eligible ||
		p.Cursor != returned.Cursor || !maps.Equal(p.Result.Skipped, returned.Result.Skipped) {
		t.Fatalf("the background tally changed the returned preview: %+v, was %+v", *p, returned)
	}
	done, err := h.svc.PreviewStatus(context.Background(), Actor{WorkspaceID: workspaceID, UserID: manager}, p.ID)
	if err != nil || done.Status != leadaction.PreviewDone || done.Result.Selected != 12000 {
		t.Fatalf("the finished preview = %+v, %v", done, err)
	}
}

func TestReadingAPreviewWhileItsTallyRunsIsSafe(t *testing.T) {
	h := newHarness(12000)
	var wg sync.WaitGroup
	h.svc.deps.Background = func(run func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run()
		}()
	}
	var mu sync.Mutex
	h.svc.deps.Now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		h.clock.at = h.clock.at.Add(4 * time.Second)
		return h.clock.at
	}
	p, err := h.svc.Preview(context.Background(), block(selection.Selection{Mode: selection.ModeAllMatching, Filter: interest()}, ""))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for i := 0; i < 200; i++ {
		for _, n := range p.Result.Skipped {
			seen += n
		}
		seen += p.Result.Selected
	}
	wg.Wait()
	if seen == 0 || p.Status != leadaction.PreviewRunning {
		t.Fatalf("the returned preview = %+v", *p)
	}
}

func TestPreviewCountsTheSameSearchTheListSends(t *testing.T) {
	h := newHarness(12)
	search := &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"Maria"}},
	}}}}
	p, err := h.svc.Preview(context.Background(), classify(`"alto"`, selection.Selection{Mode: selection.ModeAllMatching, Filter: search}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Result.Matched != 12 || !reflect.DeepEqual(h.selections.counted[0].Filter, search) {
		t.Fatalf("the preview counted %+v over %+v", p.Result, h.selections.counted[0].Filter)
	}
}

func TestExportCreatesALeadsReportOverAFrozenSnapshot(t *testing.T) {
	h := newHarness(40)
	req := Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionExport,
		Params: leadaction.Params{Addresses: true}, Selection: matchingAll(40), IdempotencyKey: "export-1", Locale: "pt"}
	out, err := h.svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Report == nil || len(h.reports.inputs) != 1 {
		t.Fatalf("export outcome = %+v", out)
	}
	in := h.reports.inputs[0]
	var params struct {
		SnapshotID string `json:"snapshotId"`
		Addresses  bool   `json:"addresses"`
		Selected   int    `json:"selected"`
	}
	_ = json.Unmarshal(in.Params, &params)
	if in.Kind != report.KindLeads || in.RequestedBy != manager || in.FromClient || !params.Addresses || params.Selected != 40 || h.selections.snapshots[params.SnapshotID] == nil {
		t.Fatalf("report input = %+v, params %+v", in, params)
	}
	if _, err := h.svc.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var again struct {
		SnapshotID string `json:"snapshotId"`
	}
	_ = json.Unmarshal(h.reports.inputs[1].Params, &again)
	if again.SnapshotID != params.SnapshotID || h.selections.freezes != 1 {
		t.Fatal("a retried export froze a second set")
	}

	h.perms[manager]["leads:read_addresses"] = false
	req.IdempotencyKey = "export-2"
	if _, err := h.svc.Start(context.Background(), req); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("an address export without read_addresses = %v", err)
	}
}

func TestMetaAudienceSendsPickedLeadsAsAnIDPredicate(t *testing.T) {
	h := newHarness(2)
	picked := selection.Selection{Mode: selection.ModeIDs, IDs: leadIDs(2)}
	req := Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionMetaAudience,
		Params: leadaction.Params{AdAccountID: "acc-1", Name: "Base"}, Selection: picked, IdempotencyKey: "aud-1"}
	out, err := h.svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Audience == nil || out.Audience.Status != leadaction.AudiencePending || len(h.audiences.drafts) != 0 {
		t.Fatalf("the audience was built inside the request: %+v, %d drafts", out.Audience, len(h.audiences.drafts))
	}
	if _, frozen := h.selections.snapshots[out.Audience.ID]; !frozen || h.selections.frozenSel[0].Require == nil {
		t.Fatal("the audience does not hold its frozen set without blocked leads")
	}
	h.drain()
	draft := h.audiences.drafts[0]
	if draft.Source != advertising.SourceCRM || draft.CRMFilter.Groups[0].Predicates[0].Field != crmfilter.FieldID || draft.CRMSnapshotID != out.Audience.ID || h.audiences.requesters[0].UserID != manager {
		t.Fatalf("draft = %+v", draft)
	}
	if _, kept := h.selections.snapshots[out.Audience.ID]; kept {
		t.Fatal("the frozen set of a built audience was kept")
	}
	done, err := h.svc.Audience(context.Background(), Actor{WorkspaceID: workspaceID, UserID: manager}, out.Audience.ID)
	if err != nil || done.Status != leadaction.AudienceDone || done.Audience.MetaID != "aud-1" || done.Matched != 2 {
		t.Fatalf("the finished audience = %+v, %v", done, err)
	}
	if _, err := h.svc.Audience(context.Background(), Actor{WorkspaceID: workspaceID, UserID: seller}, out.Audience.ID); !errors.Is(err, leadaction.ErrAudienceNotFound) {
		t.Fatalf("another member read the audience: %v", err)
	}
	if again, err := h.svc.Start(context.Background(), req); err != nil || again.Audience.Audience.MetaID != "aud-1" || len(h.audiences.drafts) != 1 {
		t.Fatalf("a retried audience = %+v, %v, %d created", again, err, len(h.audiences.drafts))
	}
	other := req
	other.Params.Name = "Outra"
	if _, err := h.svc.Start(context.Background(), other); !errors.Is(err, leadaction.ErrIdempotencyKeyReused) {
		t.Fatalf("a key reused for another audience = %v", err)
	}
	firstN := req
	firstN.IdempotencyKey = "aud-2"
	firstN.Selection = selection.Selection{Mode: selection.ModeFirstN, Filter: interest(), Limit: 1, Fingerprint: selection.Fingerprint(*interest()), ExpectedCount: 1}
	if _, err := h.svc.Start(context.Background(), firstN); !errors.Is(err, selection.ErrModeUnsupported) {
		t.Fatalf("a quantity audience = %v", err)
	}
}

func TestARunIsReadByItsActorOrCapabilityHoldersWithSensitiveValuesRedacted(t *testing.T) {
	h := newHarness(3)
	req := classify(`"Positivo"`, matchingAll(3))
	req.Params.Key = "classificacao"
	out, err := h.svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := h.svc.Run(context.Background(), Actor{WorkspaceID: workspaceID, UserID: manager}, out.Run.ID)
	if err != nil || string(mine.Params.Value) != `"Positivo"` {
		t.Fatalf("the actor reads its run = %+v, %v", mine, err)
	}
	h.perms[seller]["leads:update"] = true
	h.perms[seller]["leads:bulk_update"] = true
	theirs, err := h.svc.Run(context.Background(), Actor{WorkspaceID: workspaceID, UserID: seller}, out.Run.ID)
	if err != nil || theirs.Params.Value != nil || !theirs.Params.ValueRedacted {
		t.Fatalf("a holder without read_sensitive = %+v, %v", theirs, err)
	}
	if _, err := h.svc.Run(context.Background(), Actor{WorkspaceID: workspaceID, UserID: "stranger"}, out.Run.ID); !errors.Is(err, leadaction.ErrRunNotFound) {
		t.Fatalf("a stranger read the run: %v", err)
	}
}

func TestTheMetaPhaseKeepsItsHeartbeatWhileItWorks(t *testing.T) {
	h := newHarness(60)
	if _, err := h.svc.Start(context.Background(), block(matchingAll(60), phoneID)); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if run := h.runs.only(); run.Status != leadaction.StatusDone || run.Result.MetaApplied != 60 {
		t.Fatalf("block run = %+v", run)
	}
	if h.runs.saves < 5 {
		t.Fatalf("the run saved %d times, a long Meta phase must save its progress as it goes", h.runs.saves)
	}
}
