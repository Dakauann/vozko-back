package leadaction_usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/leadaction"
	"vozko/domain/metrics"
	"vozko/domain/selection"
)

func TestTheServiceRefusesToBuildWithoutMetrics(t *testing.T) {
	h := newHarness(1)
	deps := h.svc.deps
	deps.Metrics = nil
	if _, err := NewService(deps); err == nil || !strings.Contains(err.Error(), "metrics") {
		t.Fatalf("a service without metrics = %v", err)
	}
}

func TestAFinishedRunIsCountedWithItsDurationOnce(t *testing.T) {
	h := newHarness(1200)
	out, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(1200)))
	if err != nil {
		t.Fatal(err)
	}
	h.clock.at = start.Add(90 * time.Second)
	h.drain()
	if err := h.svc.Process(context.Background(), out.Run.ID); err != nil {
		t.Fatal(err)
	}

	if got := h.metrics.runs["classify|done|none"]; got != 1 || len(h.metrics.runs) != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
	if got := h.metrics.durations["classify|done"]; !reflect.DeepEqual(got, []time.Duration{90 * time.Second}) {
		t.Fatalf("durations = %v", h.metrics.durations)
	}
	if len(h.metrics.skips) != 0 {
		t.Fatalf("a run that changed every lead counted skips: %v", h.metrics.skips)
	}
}

func TestARepeatedRunCountsItsSkipsByReason(t *testing.T) {
	h := newHarness(600)
	if _, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(600))); err != nil {
		t.Fatal(err)
	}
	h.drain()
	retry := classify(`"alto"`, matchingAll(600))
	retry.IdempotencyKey = "key-2"
	if _, err := h.svc.Start(context.Background(), retry); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if got := h.metrics.skips["classify|"+string(leadaction.SkipUnchanged)]; got != 600 {
		t.Fatalf("skips = %v", h.metrics.skips)
	}
	if got := h.metrics.runs["classify|done|none"]; got != 2 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
}

func TestAFailedRunIsCountedWithItsFailureCode(t *testing.T) {
	h := newHarness(10)
	if _, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10))); err != nil {
		t.Fatal(err)
	}
	h.perms[manager]["leads:bulk_update"] = false
	h.drain()
	if got := h.metrics.runs["classify|failed|"+string(leadaction.FailureForbidden)]; got != 1 || len(h.metrics.runs) != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
	if got := h.metrics.durations["classify|failed"]; len(got) != 1 {
		t.Fatalf("durations = %v", h.metrics.durations)
	}
}

func TestAMetaBlockThatFailsForALeadIsCountedAsASkip(t *testing.T) {
	h := newHarness(3)
	h.phone.fail[h.writer.leads[leadIDs(3)[1]].number] = true
	if _, err := h.svc.Start(context.Background(), block(matchingAll(3), phoneID)); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if got := h.metrics.skips["block|"+metrics.LeadActionSkipMetaFailed]; got != 1 {
		t.Fatalf("skips = %v", h.metrics.skips)
	}
	if got := h.metrics.runs["block|done|none"]; got != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
}

func TestStalledRunsAreCountedByAction(t *testing.T) {
	h := newHarness(10)
	out, _ := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10)))
	h.queued = nil
	stuck, _ := h.runs.Claim(context.Background(), out.Run.ID, "dead-worker", start)
	if stuck == nil {
		t.Fatal("claim failed")
	}
	h.runs.runs[out.Run.ID].Attempts = leadaction.MaxAttempts
	h.clock.at = start.Add(leadaction.StaleAfter + time.Second)
	if err := h.svc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.metrics.runs["classify|failed|"+string(leadaction.FailureStalled)]; got != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
}

func audienceRequest(key string) Request {
	return Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionMetaAudience,
		Params: leadaction.Params{AdAccountID: "acc-1", Name: "Base"}, Selection: selection.Selection{Mode: selection.ModeIDs, IDs: leadIDs(2)}, IdempotencyKey: key}
}

func TestABuiltAudienceIsCountedWithTheLeadsWithoutAMatchKey(t *testing.T) {
	h := newHarness(2)
	h.audiences.skipped = 1
	if _, err := h.svc.Start(context.Background(), audienceRequest("aud-1")); err != nil {
		t.Fatal(err)
	}
	h.clock.at = start.Add(3 * time.Second)
	h.drain()
	if got := h.metrics.runs["meta_audience|done|none"]; got != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
	if got := h.metrics.skips["meta_audience|"+metrics.LeadActionSkipNoMatchKey]; got != 1 {
		t.Fatalf("skips = %v", h.metrics.skips)
	}
	if got := h.metrics.durations["meta_audience|done"]; !reflect.DeepEqual(got, []time.Duration{3 * time.Second}) {
		t.Fatalf("durations = %v", h.metrics.durations)
	}
	if _, err := h.svc.Start(context.Background(), audienceRequest("aud-1")); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if got := h.metrics.runs["meta_audience|done|none"]; got != 1 {
		t.Fatalf("a retried audience was counted again: %v", h.metrics.runs)
	}
}

func TestAFailedAudienceIsCountedWithItsCode(t *testing.T) {
	h := newHarness(2)
	h.audiences.err = errors.New("meta down")
	if _, err := h.svc.Start(context.Background(), audienceRequest("aud-1")); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if got := h.metrics.runs["meta_audience|failed|"+string(leadaction.FailureInternal)]; got != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
}

func TestAnExportIsCountedOnceWhenItsReportIsCreated(t *testing.T) {
	h := newHarness(40)
	req := Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionExport,
		Selection: matchingAll(40), IdempotencyKey: "export-1", Locale: "pt"}
	for i := 0; i < 2; i++ {
		h.clock.at = h.clock.at.Add(time.Second)
		if _, err := h.svc.Start(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.metrics.runs["export|created|none"]; got != 1 || len(h.metrics.durations["export|created"]) != 1 {
		t.Fatalf("runs = %v, durations = %v", h.metrics.runs, h.metrics.durations)
	}
}

func TestAnExportWhoseReportFailedIsCountedWhenTheRetryCreatesIt(t *testing.T) {
	h := newHarness(40)
	req := Request{Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionExport,
		Selection: matchingAll(40), IdempotencyKey: "export-1", Locale: "pt"}
	h.reports.failNextBy = errors.New("report queue down")
	if _, err := h.svc.Start(context.Background(), req); err == nil {
		t.Fatal("an export whose report was not created succeeded")
	}
	if len(h.metrics.runs) != 0 {
		t.Fatalf("an export without a report was counted: %v", h.metrics.runs)
	}
	for i := 0; i < 2; i++ {
		h.clock.at = h.clock.at.Add(time.Second)
		if _, err := h.svc.Start(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.metrics.runs["export|created|none"]; got != 1 || len(h.metrics.durations["export|created"]) != 1 || len(h.reports.jobs) != 1 {
		t.Fatalf("runs = %v, durations = %v, reports = %d", h.metrics.runs, h.metrics.durations, len(h.reports.jobs))
	}
}

func TestACallListIsCountedOnceWhenItIsCreated(t *testing.T) {
	h := newHarness(4)
	for i := 0; i < 2; i++ {
		if _, err := h.svc.Start(context.Background(), callListRequest("call-list-1", 4)); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.metrics.runs["call_list|created|none"]; got != 1 || len(h.metrics.durations["call_list|created"]) != 1 {
		t.Fatalf("runs = %v, durations = %v", h.metrics.runs, h.metrics.durations)
	}
}

func TestARefusedStartCountsNothing(t *testing.T) {
	h := newHarness(10)
	h.perms[manager]["leads:bulk_update"] = false
	if _, err := h.svc.Start(context.Background(), classify(`"alto"`, matchingAll(10))); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("a start without the capability = %v", err)
	}
	if len(h.metrics.runs) != 0 || len(h.metrics.durations) != 0 || len(h.metrics.skips) != 0 {
		t.Fatalf("a refused start was counted: %+v", h.metrics)
	}
}
