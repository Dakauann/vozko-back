package calllist_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/calllist"
	"vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/sip_trunk"
)

func TestTheServiceRefusesToBuildWithoutItsPorts(t *testing.T) {
	h := newHarness()
	full := Deps{Store: h.store, Leads: h.leads, Snapshots: h.snapshots, Calls: h.calls, Outcomes: h.outcomes,
		Interactions: h.interactions, Access: h.access, Lines: h.lines, Permissions: h.grants}
	if _, err := NewService(full); err != nil {
		t.Fatalf("complete deps = %v", err)
	}
	for name, strip := range map[string]func(*Deps){
		"store": func(d *Deps) { d.Store = nil }, "leads": func(d *Deps) { d.Leads = nil },
		"snapshots": func(d *Deps) { d.Snapshots = nil }, "calls": func(d *Deps) { d.Calls = nil }, "outcomes": func(d *Deps) { d.Outcomes = nil },
		"interactions": func(d *Deps) { d.Interactions = nil }, "access": func(d *Deps) { d.Access = nil }, "lines": func(d *Deps) { d.Lines = nil },
		"permissions": func(d *Deps) { d.Permissions = nil },
	} {
		deps := full
		strip(&deps)
		if _, err := NewService(deps); err == nil {
			t.Errorf("a service without %s was built", name)
		}
	}
}

func draft() calllist.Draft {
	return calllist.Draft{ID: "list-1", Name: "Retorno", AssigneeIDs: []string{worker, colleague}, Phone: calllist.PhoneChoice{Source: calllist.PhoneIdentity}}
}

func TestPreparingAListChecksTheCreatorAndTheMembers(t *testing.T) {
	cases := []struct {
		name   string
		actor  string
		change func(*calllist.Draft, *harness)
		want   error
	}{
		{"a manager with members who can call", manager, func(*calllist.Draft, *harness) {}, nil},
		{"someone who does not manage lists", worker, func(*calllist.Draft, *harness) {}, calllist.ErrForbidden},
		{"a member who cannot call", manager, func(d *calllist.Draft, _ *harness) { d.AssigneeIDs = []string{worker, stranger} }, calllist.ErrAssigneeCannotWork},
		{"no member", manager, func(d *calllist.Draft, _ *harness) { d.AssigneeIDs = nil }, calllist.ErrAssigneesRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			d := draft()
			tc.change(&d, h)
			got, err := h.service().Prepare(context.Background(), actor(tc.actor), d)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Prepare = %v, want %v", err, tc.want)
			}
			if err == nil && (got.Draft().WorkspaceID != ws || got.Draft().CreatedBy != tc.actor) {
				t.Fatalf("prepared draft = %+v, want it bound to the actor and workspace", got)
			}
		})
	}
}

func seedSnapshot(h *harness, listID string, leads ...*lead.Lead) {
	for _, l := range leads {
		if l.WorkspaceID != "" {
			h.leads.leads[l.ID] = l
		}
		h.snapshots.ids[listID] = append(h.snapshots.ids[listID], l.ID)
	}
}

func TestCreatingFromASnapshotBuildsTheItemsAndSkipsWhatTheRuleRefuses(t *testing.T) {
	h := newHarness()
	optedOut := now.Add(-time.Hour)
	blocked := callable("lead-b", "5511987650002")
	blocked.Blocked = true
	out := callable("lead-c", "5511987650003")
	out.OptedOutAt = &optedOut
	noNumber := &lead.Lead{ID: "lead-d", WorkspaceID: ws, Name: "Sem telefone"}
	gone := &lead.Lead{ID: "lead-e"}
	seedSnapshot(h, "list-1", callable("lead-a", "5511987650001"), blocked, out, noNumber, gone, callable("lead-f", "5511987650006"))

	svc := h.service()
	prepared, err := svc.Prepare(context.Background(), actor(manager), draft())
	if err != nil {
		t.Fatal(err)
	}
	l, err := svc.CreateFromSnapshot(context.Background(), actor(manager), prepared, 6)
	if err != nil {
		t.Fatal(err)
	}
	if l.ID != "list-1" || l.Status != calllist.StatusBuilding {
		t.Fatalf("created = %+v, want a list still building", l)
	}
	if h.launched != 1 {
		t.Fatalf("builds launched = %d", h.launched)
	}
	built, _ := h.store.Get(context.Background(), ws, "list-1")
	if built.Status != calllist.StatusActive || built.ItemCount != 2 {
		t.Fatalf("built = %+v", built)
	}
	want := calllist.Skips{calllist.SkipReason(lead.DialRefusedBlocked): 1, calllist.SkipReason(lead.DialRefusedOptedOut): 1, calllist.SkipNoNumber: 1, calllist.SkipGone: 1}
	for reason, count := range want {
		if built.Skipped[reason] != count {
			t.Fatalf("skipped = %v, want %v", built.Skipped, want)
		}
	}
	if len(h.snapshots.dropped) != 1 || h.snapshots.dropped[0] != "list-1" {
		t.Fatalf("dropped snapshots = %v", h.snapshots.dropped)
	}
	if len(h.leads.identityAsk) != 0 {
		t.Fatal("a list on the WhatsApp number never needs the holders of other numbers")
	}

	again, err := svc.CreateFromSnapshot(context.Background(), actor(manager), prepared, 6)
	if err != nil || again.ID != "list-1" || h.launched != 1 {
		t.Fatalf("a retried create = %+v, %v, launched %d", again, err, h.launched)
	}
}

func TestAListOnAContactPhoneChecksTheHoldersOfThoseNumbers(t *testing.T) {
	h := newHarness()
	owner := callable("lead-owner", "551133334444")
	owner.Blocked = true
	h.leads.identities = []*lead.Lead{owner}
	shared := callable("lead-a", "5511987650001")
	shared.Phones = []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}
	own := callable("lead-b", "5511987650002")
	own.Phones = []lead.ContactPhone{{Number: "551155556666", Label: lead.PhoneLandline}}
	seedSnapshot(h, "list-1", shared, own)
	d := draft()
	d.Phone = calllist.PhoneChoice{Source: calllist.PhoneContact, Label: lead.PhoneLandline}
	svc := h.service()
	prepared, err := svc.Prepare(context.Background(), actor(manager), d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateFromSnapshot(context.Background(), actor(manager), prepared, 2); err != nil {
		t.Fatal(err)
	}
	built, _ := h.store.Get(context.Background(), ws, "list-1")
	if built.ItemCount != 1 || built.Skipped[calllist.SkipReason(lead.DialRefusedBlocked)] != 1 {
		t.Fatalf("built = %+v", built)
	}
	if len(h.leads.identityAsk) != 1 || len(h.leads.identityAsk[0]) != 2 {
		t.Fatalf("identity lookups = %v, want one lookup of both landlines", h.leads.identityAsk)
	}
	if h.store.appended[0].Items[0].Phone != "551155556666" {
		t.Fatalf("items = %+v", h.store.appended[0].Items)
	}
}

func TestABuildWithNoCallableLeadFailsWithItsReason(t *testing.T) {
	h := newHarness()
	blocked := callable("lead-a", "5511987650001")
	blocked.Blocked = true
	seedSnapshot(h, "list-1", blocked)
	svc := h.service()
	prepared, _ := svc.Prepare(context.Background(), actor(manager), draft())
	if _, err := svc.CreateFromSnapshot(context.Background(), actor(manager), prepared, 1); err != nil {
		t.Fatal(err)
	}
	built, _ := h.store.Get(context.Background(), ws, "list-1")
	if built.Status != calllist.StatusFailed || built.FailureCode != calllist.FailureEmpty {
		t.Fatalf("built = %+v", built)
	}
}

func TestABuildThatLostItsClaimStopsWithoutFinishing(t *testing.T) {
	h := newHarness()
	seedSnapshot(h, "list-1", callable("lead-a", "5511987650001"))
	h.store.appendErr = calllist.ErrBuildClaimLost
	svc := h.service()
	prepared, _ := svc.Prepare(context.Background(), actor(manager), draft())
	if _, err := svc.CreateFromSnapshot(context.Background(), actor(manager), prepared, 1); err != nil {
		t.Fatal(err)
	}
	if len(h.store.finished) != 0 || len(h.snapshots.dropped) != 0 {
		t.Fatalf("finished %v, dropped %v: a lost build must leave both to the owner of the claim", h.store.finished, h.snapshots.dropped)
	}
}

func TestTheSweepResumesStalledBuilds(t *testing.T) {
	h := newHarness()
	seedSnapshot(h, "list-1", callable("lead-a", "5511987650001"))
	l, _ := calllist.NewList(calllist.Draft{ID: "list-1", WorkspaceID: ws, Name: "L", CreatedBy: manager, AssigneeIDs: []string{worker},
		Phone: calllist.PhoneChoice{Source: calllist.PhoneIdentity}}, 1, now)
	h.store.lists["list-1"] = l
	if err := h.service().Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	built, _ := h.store.Get(context.Background(), ws, "list-1")
	if built.Status != calllist.StatusActive || built.ItemCount != 1 {
		t.Fatalf("built = %+v", built)
	}
}

func pendingItem(id, listID, leadID string, position int) calllist.Item {
	return calllist.Item{ID: id, ListID: listID, WorkspaceID: ws, LeadID: leadID, Phone: leadNumber(leadID), Position: position, State: calllist.StatePending, CreatedAt: now}
}

func leadNumber(leadID string) string {
	return map[string]string{"lead-a": "5511987650001", "lead-b": "5511987650002", "lead-c": "5511987650003"}[leadID]
}

func workingHarness() *harness {
	h := newHarness()
	h.store.lists["list-1"] = activeList("list-1", worker, colleague)
	a, b := callable("lead-a", "5511987650001"), callable("lead-b", "5511987650002")
	h.leads.leads["lead-a"], h.leads.leads["lead-b"] = a, b
	h.store.put(pendingItem("item-1", "list-1", "lead-a", 1), pendingItem("item-2", "list-1", "lead-b", 2))
	return h
}

func TestNextHandsAnAssigneeTheNextItemWithTheFixedLeadCardAndTheLines(t *testing.T) {
	h := workingHarness()
	h.interactions["lead-a"] = &calllist.LastInteraction{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, At: now.Add(-time.Hour)}
	h.access[worker+":entry-1"] = true

	got, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Item == nil || got.Item.ID != "item-1" || got.Item.ReservedBy != worker {
		t.Fatalf("item = %+v", got.Item)
	}
	if got.Lead != (calllist.LeadCard{ID: "lead-a", Name: "Pessoa lead-a", FamilyCount: 2}) {
		t.Fatalf("lead card = %+v", got.Lead)
	}
	if got.LastInteraction == nil || got.LastInteraction.EntryID != "entry-1" {
		t.Fatalf("last interaction = %+v", got.LastInteraction)
	}
	if len(got.Trunks) != 1 || len(h.lines.asked) != 1 || h.lines.asked[0] != "5511987650001" {
		t.Fatalf("trunks = %+v asked %v", got.Trunks, h.lines.asked)
	}
}

func TestNextHidesALastInteractionTheWorkerCannotOpen(t *testing.T) {
	h := workingHarness()
	h.interactions["lead-a"] = &calllist.LastInteraction{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, At: now}
	got, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastInteraction != nil {
		t.Fatalf("a conversation the worker cannot open leaked: %+v", got.LastInteraction)
	}
}

func TestNextClosesItemsWhoseLeadCannotBeCalledAnyMoreAndServesTheNextOne(t *testing.T) {
	h := workingHarness()
	h.leads.leads["lead-a"].Blocked = true
	got, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Item == nil || got.Item.ID != "item-2" || got.Refused != 1 {
		t.Fatalf("next = %+v refused %d", got.Item, got.Refused)
	}
	refused, _ := h.store.Item(context.Background(), ws, "item-1")
	if refused.State != calllist.StateClosed || refused.Disposition != calllist.DispositionRefused || refused.Refusal != string(lead.DialRefusedBlocked) {
		t.Fatalf("refused item = %+v", refused)
	}
	if h.store.lists["list-1"].ClosedCount != 1 {
		t.Fatalf("closed count = %d", h.store.lists["list-1"].ClosedCount)
	}
}

func TestNextRefusesALineThatCannotDialTheNumberAsInvalid(t *testing.T) {
	h := workingHarness()
	h.lines.err = sip_trunk.ErrInvalidPhoneNumber
	got, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Item != nil || got.Refused != 2 {
		t.Fatalf("next = %+v, refused %d", got.Item, got.Refused)
	}
}

func TestNextOnAnEmptyQueueAnswersWithoutAnItem(t *testing.T) {
	h := newHarness()
	h.store.lists["list-1"] = activeList("list-1", worker)
	got, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil || got.Item != nil || got.List == nil {
		t.Fatalf("next = %+v, %v", got, err)
	}
}

func TestOnlyAnAssigneeWithTheWorkCapabilityTakesItemsOfAnActiveList(t *testing.T) {
	cases := []struct {
		name   string
		user   string
		status calllist.Status
		grants func(grants)
		want   error
	}{
		{"an assignee", worker, calllist.StatusActive, func(grants) {}, nil},
		{"someone not assigned", stranger, calllist.StatusActive, func(g grants) { g[stranger] = workerGrants() }, calllist.ErrNotAssignee},
		{"an assignee without the work capability", worker, calllist.StatusActive, func(g grants) { delete(g[worker], "sip_trunks:call") }, calllist.ErrForbidden},
		{"a paused list", worker, calllist.StatusPaused, func(grants) {}, calllist.ErrListNotActive},
		{"a list still building", worker, calllist.StatusBuilding, func(grants) {}, calllist.ErrListBuilding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := workingHarness()
			h.store.lists["list-1"].Status = tc.status
			tc.grants(h.grants)
			_, err := h.service().Next(context.Background(), actor(tc.user), "list-1")
			if !errors.Is(err, tc.want) {
				t.Fatalf("Next = %v, want %v", err, tc.want)
			}
		})
	}
}

func reservedHarness(t *testing.T) (*harness, *Service) {
	t.Helper()
	h := workingHarness()
	svc := h.service()
	if _, err := svc.Next(context.Background(), actor(worker), "list-1"); err != nil {
		t.Fatal(err)
	}
	return h, svc
}

func TestTheDialCheckAdmitsOnlyTheReserverOfAnActiveList(t *testing.T) {
	h, svc := reservedHarness(t)
	if err := svc.CheckItemDial(context.Background(), dialOf(worker, "item-1", "lead-a", "5511987650001")); err != nil {
		t.Fatalf("CheckItemDial = %v", err)
	}
	cases := []struct {
		name string
		dial func() error
		want error
	}{
		{"a colleague", func() error {
			err := svc.CheckItemDial(context.Background(), dialOf(colleague, "item-1", "lead-a", "5511987650001"))
			return err
		}, calllist.ErrItemNotReserved},
		{"another lead", func() error {
			err := svc.CheckItemDial(context.Background(), dialOf(worker, "item-1", "lead-b", "5511987650001"))
			return err
		}, calllist.ErrItemMismatch},
		{"another workspace", func() error {
			d := dialOf(worker, "item-1", "lead-a", "5511987650001")
			d.WorkspaceID = "ws-2"
			err := svc.CheckItemDial(context.Background(), d)
			return err
		}, calllist.ErrItemNotFound},
		{"a paused list", func() error {
			h.store.lists["list-1"].Status = calllist.StatusPaused
			defer func() { h.store.lists["list-1"].Status = calllist.StatusActive }()
			err := svc.CheckItemDial(context.Background(), dialOf(worker, "item-1", "lead-a", "5511987650001"))
			return err
		}, calllist.ErrListNotActive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.dial(); !errors.Is(err, tc.want) {
				t.Fatalf("CheckItemDial = %v, want %v", err, tc.want)
			}
		})
	}
	item, _ := h.store.Item(context.Background(), ws, "item-1")
	if item.LastCallID != "" {
		t.Fatal("the dial check is read-only")
	}
}

func TestTheCallRecordIsStampedOnTheItem(t *testing.T) {
	h, svc := reservedHarness(t)
	h.clock = now.Add(10 * time.Minute)
	if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-1")); err != nil {
		t.Fatal(err)
	}
	item, _ := h.store.Item(context.Background(), ws, "item-1")
	if item.LastCallID != "call-1" || !item.ReservedUntil.Equal(h.clock.Add(calllist.ReservationTTL)) {
		t.Fatalf("item = %+v", item)
	}
	if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-9", "call-1")); !errors.Is(err, calllist.ErrItemNotFound) {
		t.Fatalf("stamping a missing item = %v", err)
	}
}

func callOf(id, agent, leadID string) *cdr.Call {
	return &cdr.Call{ID: id, WorkspaceID: ws, AgentID: &agent, LeadID: &leadID, Status: cdr.StatusCompleted, Direction: cdr.DirectionOutbound}
}

func TestClosingRefusesACallThatIsNotTheItemsOwn(t *testing.T) {
	cases := []struct {
		name string
		call *cdr.Call
		want error
	}{
		{"the item's call", callOf("call-1", worker, "lead-a"), nil},
		{"a call to another lead", callOf("call-1", worker, "lead-b"), calllist.ErrCallNotTheItems},
		{"a call made by a colleague", callOf("call-1", colleague, "lead-a"), calllist.ErrCallNotTheItems},
		{"a call of another workspace", func() *cdr.Call { c := callOf("call-1", worker, "lead-a"); c.WorkspaceID = "ws-2"; return c }(), calllist.ErrCallNotTheItems},
		{"a call record that is gone", nil, calllist.ErrCallNotTheItems},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, svc := reservedHarness(t)
			if tc.call != nil {
				h.calls["call-1"] = tc.call
			}
			if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-1")); err != nil {
				t.Fatal(err)
			}
			item, err := svc.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: "interessado", Note: "retornar"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Close = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (item.State != calllist.StateClosed || item.ClosedBy != worker || h.store.lists["list-1"].ClosedCount != 1) {
				t.Fatalf("closed = %+v", item)
			}
		})
	}
}

func TestClosingNeedsACallTheWorkCapabilityAndTheWorkspaceOutcomes(t *testing.T) {
	h, svc := reservedHarness(t)
	if _, err := svc.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: "interessado"}); !errors.Is(err, calllist.ErrItemNotCalled) {
		t.Fatalf("closing without a call = %v", err)
	}
	h.calls["call-1"] = callOf("call-1", worker, "lead-a")
	_ = svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-1"))
	h.outcomes.err = errBoom
	if _, err := h.service().Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: "interessado"}); !errors.Is(err, errBoom) {
		t.Fatalf("unreadable outcomes = %v", err)
	}
	h.outcomes.err = nil
	delete(h.grants[worker], "call_session:use")
	if _, err := h.service().Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: "interessado"}); !errors.Is(err, calllist.ErrForbidden) {
		t.Fatalf("without the work capability = %v", err)
	}
}

func TestACallbackPutsTheItemBackForLater(t *testing.T) {
	h, svc := reservedHarness(t)
	h.calls["call-1"] = callOf("call-1", worker, "lead-a")
	_ = svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-1"))
	when := now.Add(2 * time.Hour)
	item, err := svc.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: calllist.DispositionCallback, CallbackAt: &when})
	if err != nil {
		t.Fatal(err)
	}
	if item.State != calllist.StatePending || h.store.lists["list-1"].ClosedCount != 0 {
		t.Fatalf("callback = %+v", item)
	}
}

func TestReleasingGivesTheItemBackToTheQueue(t *testing.T) {
	h, svc := reservedHarness(t)
	if _, err := svc.Release(context.Background(), actor(colleague), "item-1"); !errors.Is(err, calllist.ErrItemNotReserved) {
		t.Fatalf("a colleague releasing = %v", err)
	}
	item, err := svc.Release(context.Background(), actor(worker), "item-1")
	if err != nil || item.State != calllist.StatePending {
		t.Fatalf("release = %+v, %v", item, err)
	}
	got, err := svc.Next(context.Background(), actor(colleague), "list-1")
	if err != nil || got.Item == nil || got.Item.ID != "item-1" {
		t.Fatalf("the released item for the colleague = %+v, %v", got, err)
	}
	_ = h
}

func TestListsAreVisibleToAssigneesAndManagers(t *testing.T) {
	h := newHarness()
	h.store.lists["list-1"] = activeList("list-1", worker)
	h.store.lists["list-2"] = activeList("list-2", colleague)
	svc := h.service()
	mine, err := svc.Lists(context.Background(), actor(worker), calllist.ListQuery{})
	if err != nil || mine.Total != 1 || mine.Lists[0].ID != "list-1" {
		t.Fatalf("worker lists = %+v, %v", mine, err)
	}
	all, err := svc.Lists(context.Background(), actor(manager), calllist.ListQuery{})
	if err != nil || all.Total != 2 {
		t.Fatalf("manager lists = %+v, %v", all, err)
	}
	if _, err := svc.Get(context.Background(), actor(worker), "list-2"); !errors.Is(err, calllist.ErrListNotFound) {
		t.Fatalf("a list of others = %v", err)
	}
	if _, err := svc.Get(context.Background(), actor(manager), "list-2"); err != nil {
		t.Fatalf("a manager reading any list = %v", err)
	}
	h.grants[worker] = map[string]bool{}
	if _, err := svc.Lists(context.Background(), actor(worker), calllist.ListQuery{}); !errors.Is(err, calllist.ErrForbidden) {
		t.Fatalf("without call_lists:read = %v", err)
	}
}

func TestItemsCarryTheTechnicalOutcomeDerivedFromTheirCall(t *testing.T) {
	h := workingHarness()
	h.store.items["item-1"].LastCallID = "call-1"
	page, err := h.service().Items(context.Background(), actor(worker), calllist.ItemQuery{ListID: "list-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Outcome != callhistory.OutcomeAnswered || page.Items[1].Outcome != "" {
		t.Fatalf("items = %+v", page.Items)
	}
	if _, err := h.service().Items(context.Background(), actor(stranger), calllist.ItemQuery{ListID: "list-1"}); !errors.Is(err, calllist.ErrListNotFound) {
		t.Fatalf("items of a list of others = %v", err)
	}
}

func TestOnlyManagersChangeOrDeleteLists(t *testing.T) {
	h := workingHarness()
	svc := h.service()
	paused := calllist.StatusPaused
	if _, err := svc.Update(context.Background(), actor(worker), "list-1", calllist.Change{Status: &paused}); !errors.Is(err, calllist.ErrForbidden) {
		t.Fatalf("a worker pausing = %v", err)
	}
	updated, err := svc.Update(context.Background(), actor(manager), "list-1", calllist.Change{Status: &paused})
	if err != nil || updated.Status != calllist.StatusPaused {
		t.Fatalf("pause = %+v, %v", updated, err)
	}
	members := []string{stranger}
	if _, err := svc.Update(context.Background(), actor(manager), "list-1", calllist.Change{AssigneeIDs: &members}); !errors.Is(err, calllist.ErrAssigneeCannotWork) {
		t.Fatalf("assigning someone who cannot call = %v", err)
	}
	if err := svc.Delete(context.Background(), actor(worker), "list-1"); !errors.Is(err, calllist.ErrForbidden) {
		t.Fatalf("a worker deleting = %v", err)
	}
	if err := svc.Delete(context.Background(), actor(manager), "list-1"); err != nil {
		t.Fatal(err)
	}
}

func dialOf(user, itemID, leadID, number string) callsession.CallListItemDial {
	return callsession.CallListItemDial{WorkspaceID: ws, UserID: user, ItemID: itemID, LeadID: leadID, Number: number}
}
