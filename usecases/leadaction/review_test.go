package leadaction_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/selection"
	lead_usecase "vozko/usecases/lead"
)

func firstN(limit, expected int) selection.Selection {
	f := interest()
	return selection.Selection{Mode: selection.ModeFirstN, Filter: f, Limit: limit, Fingerprint: selection.Fingerprint(*f), ExpectedCount: expected}
}

func TestStartRefusesAFilteredSelectionWithoutItsCount(t *testing.T) {
	h := newHarness(10)
	uncounted := matchingAll(0)
	if _, err := h.svc.Start(context.Background(), classify(`"alto"`, uncounted)); !errors.Is(err, selection.ErrCountRequired) {
		t.Fatalf("a filter without its count = %v", err)
	}
	if _, err := h.svc.Start(context.Background(), classify(`"alto"`, firstN(5, 0))); !errors.Is(err, selection.ErrCountRequired) {
		t.Fatalf("a quantity without its count = %v", err)
	}
	picked := classify(`"alto"`, selection.Selection{Mode: selection.ModeIDs, IDs: leadIDs(2)})
	if _, err := h.svc.Start(context.Background(), picked); err != nil {
		t.Fatalf("picked leads need no count: %v", err)
	}
	if h.runs.create != 1 {
		t.Fatalf("%d runs created", h.runs.create)
	}
}

func TestTwoRequestsRacingOnOneKeyNeverShareAFrozenSet(t *testing.T) {
	h := newHarness(10)
	first, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10)))
	if err != nil {
		t.Fatal(err)
	}
	h.runs.hideKeys = 1
	if _, err := h.svc.Start(context.Background(), classify(`"baixo"`, matchingAll(10))); !errors.Is(err, leadaction.ErrIdempotencyKeyReused) {
		t.Fatalf("the loser of the race = %v", err)
	}
	if h.selections.freezes != 2 || len(h.selections.dropped) != 1 || h.selections.dropped[0] == first.Run.ID {
		t.Fatalf("freezes %d, dropped %v, winner %s", h.selections.freezes, h.selections.dropped, first.Run.ID)
	}
	if _, kept := h.selections.snapshots[first.Run.ID]; !kept {
		t.Fatal("the loser dropped the winner's frozen set")
	}
	h.drain()
	if run := h.runs.only(); run.Status != leadaction.StatusDone || run.Params.Value == nil || string(run.Params.Value) != `"alto"` {
		t.Fatalf("the winner ran %+v", run)
	}
}

func TestABlockOnAPhoneTheWorkspaceCannotUseIsRefusedBeforeFreezing(t *testing.T) {
	h := newHarness(5)
	h.metaPhones.err = fmt.Errorf("%w: foreign", lead_usecase.ErrBlockingPhoneUnavailable)
	if _, err := h.svc.Start(context.Background(), block(matchingAll(5), phoneID)); !errors.Is(err, leadaction.ErrPhoneUnavailable) {
		t.Fatalf("a foreign phone = %v", err)
	}
	if _, err := h.svc.Preview(context.Background(), block(matchingAll(5), phoneID)); !errors.Is(err, leadaction.ErrPhoneUnavailable) {
		t.Fatalf("a preview over a foreign phone = %v", err)
	}
	h.metaPhones.err = nil
	if _, err := h.svc.Start(context.Background(), block(matchingAll(5), "not-a-phone")); !errors.Is(err, leadaction.ErrPhoneUnavailable) {
		t.Fatalf("a malformed phone id = %v", err)
	}
	h.metaPhones.err = errors.New("connection reset")
	if _, err := h.svc.Start(context.Background(), block(matchingAll(5), phoneID)); err == nil || errors.Is(err, leadaction.ErrPhoneUnavailable) {
		t.Fatalf("a failed phone read = %v", err)
	}
	if h.selections.freezes != 0 || h.runs.create != 0 {
		t.Fatal("a refused block froze or stored something")
	}
}

func TestAFailedPhoneReadReleasesTheRunInsteadOfSkippingTheMetaBlock(t *testing.T) {
	h := newHarness(5)
	if _, err := h.svc.Start(context.Background(), block(matchingAll(5), phoneID)); err != nil {
		t.Fatal(err)
	}
	h.metaPhones.err = errors.New("connection reset")
	h.drain()
	run := h.runs.only()
	if run.Status != leadaction.StatusQueued || run.Phase != leadaction.PhaseMeta || run.Result.MetaUnavailable {
		t.Fatalf("a run after a failed phone read = %+v", run)
	}
	h.metaPhones.err = nil
	if err := h.svc.Process(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if done := h.runs.only(); done.Status != leadaction.StatusDone || done.Result.MetaApplied != 5 {
		t.Fatalf("the retried run = %+v", done)
	}

	gone := newHarness(3)
	if _, err := gone.svc.Start(context.Background(), block(matchingAll(3), phoneID)); err != nil {
		t.Fatal(err)
	}
	gone.metaPhones.err = lead_usecase.ErrBlockingPhoneUnavailable
	gone.drain()
	if run := gone.runs.only(); run.Status != leadaction.StatusDone || !run.Result.MetaUnavailable {
		t.Fatalf("a phone that went away = %+v", run)
	}
}

func TestAHundredMetaBlocksUnderALimitOfTwentyFinishWithoutTheSweep(t *testing.T) {
	h := newHarness(100)
	h.limiter.allow, h.limiter.refill, h.limiter.retry = 20, 20, time.Second
	if _, err := h.svc.Start(context.Background(), block(matchingAll(100), phoneID)); err != nil {
		t.Fatal(err)
	}
	h.drain()
	run := h.runs.only()
	if run.Status != leadaction.StatusDone || run.Result.MetaApplied != 100 || len(h.phone.applied) != 100 {
		t.Fatalf("block run = %+v", run)
	}
	if len(h.slept) != 4 || len(h.scheduled) != 0 {
		t.Fatalf("waited %v, scheduled %d", h.slept, len(h.scheduled))
	}
}

func TestAMetaPhaseWhoseFrozenSetIsGoneFailsWithACode(t *testing.T) {
	h := newHarness(5)
	h.limiter.allow = 2
	if _, err := h.svc.Start(context.Background(), block(matchingAll(5), phoneID)); err != nil {
		t.Fatal(err)
	}
	h.drain()
	run := h.runs.only()
	delete(h.selections.snapshots, run.ID)
	h.limiter.allow = 10
	h.clock.at = *run.NotBefore
	h.scheduled[0].run()
	h.drain()
	failed := h.runs.only()
	if failed.Status != leadaction.StatusFailed || failed.FailureCode != leadaction.FailureSnapshotLost {
		t.Fatalf("a run without its frozen set = %+v", failed)
	}
}

func TestAnExportOverTheCapIsRefusedBeforeFreezing(t *testing.T) {
	h := newHarness(3)
	h.selections.matched = leadaction.MaxExportLeads + 1
	req := Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionExport,
		Selection: matchingAll(leadaction.MaxExportLeads + 1), IdempotencyKey: "export-big"}
	if _, err := h.svc.Start(context.Background(), req); !errors.Is(err, leadaction.ErrSelectionTooLarge) {
		t.Fatalf("an oversized export = %v", err)
	}
	if h.selections.freezes != 0 || len(h.reports.inputs) != 0 {
		t.Fatal("an oversized export froze its selection")
	}
}

func TestAQuantityRunFreezesOnlyLeadsTheEditWouldChange(t *testing.T) {
	h := newHarness(10)
	if _, err := h.svc.Start(context.Background(), block(firstN(5, 5), "")); err != nil {
		t.Fatal(err)
	}
	want := &lead.Assignment{Kind: lead.AssignBlocked, Value: true}
	if len(h.selections.pendings) != 1 || !reflect.DeepEqual(h.selections.pendings[0], want) {
		t.Fatalf("pending = %#v", h.selections.pendings)
	}
	filtered := newHarness(10)
	if _, err := filtered.svc.Start(context.Background(), block(matchingAll(10), "")); err != nil {
		t.Fatal(err)
	}
	if filtered.selections.pendings[0] != nil {
		t.Fatal("a filter selection lost its unchanged leads before they were counted")
	}
}

func TestAQuantityPreviewFreezesItsOrderedHeadOnce(t *testing.T) {
	h := newHarness(12000)
	p, err := h.svc.Preview(context.Background(), block(firstN(12000, 0), ""))
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != leadaction.PreviewDone || p.Result.Selected != 12000 {
		t.Fatalf("preview = %+v", p)
	}
	if h.selections.freezes != 1 || len(h.selections.resolved) != 0 || len(h.selections.paged) != 3 {
		t.Fatalf("freezes %d, resolved %d, snapshot pages %d", h.selections.freezes, len(h.selections.resolved), len(h.selections.paged))
	}
	if len(h.selections.dropped) != 1 || h.selections.dropped[0] != p.ID || h.selections.pendings[0] == nil {
		t.Fatalf("the preview head stays: dropped %v", h.selections.dropped)
	}
}

func TestAnExportPreviewCountsOnceWithoutPaging(t *testing.T) {
	h := newHarness(7000)
	req := Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionExport,
		Selection: selection.Selection{Mode: selection.ModeAllMatching, Filter: interest(), ExcludeIDs: leadIDs(1)}}
	p, err := h.svc.Preview(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != leadaction.PreviewDone || p.Result.Selected != 7000 || p.Result.Eligible != 7000 || p.Result.Matched != 7000 {
		t.Fatalf("preview = %+v", p)
	}
	if len(h.selections.selected) != 1 || len(h.selections.resolved) != 0 || h.selections.freezes != 0 {
		t.Fatalf("selected %d, resolved %d, freezes %d", len(h.selections.selected), len(h.selections.resolved), h.selections.freezes)
	}
	if got := h.selections.selected[0].ExcludeIDs; len(got) != 1 {
		t.Fatalf("the export count lost its exclusions: %v", got)
	}
}
