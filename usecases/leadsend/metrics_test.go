package leadsend_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	"vozko/domain/metrics"
)

func TestTheServiceRefusesToBuildWithoutMetrics(t *testing.T) {
	deps := newHarness(nil).deps
	deps.Metrics = nil
	if _, err := NewService(deps); err == nil || !strings.Contains(err.Error(), "metrics") {
		t.Fatalf("a service without metrics = %v", err)
	}
}

func TestAPreparedSendIsCountedOnceWithItsSkipsByReason(t *testing.T) {
	h := newHarness(nil)
	ids := h.seed(7)
	h.leads.byID[ids[2]].Name = ""
	h.world.running[ids[4]] = true

	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := h.metrics.runs["send_template|"+metrics.LeadActionPrepared+"|"+metrics.LeadActionNoFailure]; got != 1 || len(h.metrics.runs) != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
	if h.metrics.durations["send_template|"+metrics.LeadActionPrepared] != 1 {
		t.Fatalf("durations = %v", h.metrics.durations)
	}
	want := map[string]int{
		"send_template|" + string(campaign.SkipMissingVariable):          1,
		"send_template|" + string(campaign.SkipAlreadyInRunningCampaign): 1,
	}
	if len(h.metrics.skips) != len(want) {
		t.Fatalf("skips = %v, want %v", h.metrics.skips, want)
	}
	for key, n := range want {
		if h.metrics.skips[key] != n {
			t.Fatalf("skips = %v, want %v", h.metrics.skips, want)
		}
	}
}

func TestARefusedPreparationCountsNothing(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)
	incomplete := templateParams()
	incomplete.TemplateID = ""
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, incomplete); err == nil {
		t.Fatal("a send without its template was prepared")
	}
	if len(h.metrics.runs) != 0 || len(h.metrics.skips) != 0 || len(h.metrics.durations) != 0 {
		t.Fatalf("a refused preparation was counted: %+v", h.metrics)
	}
}

func TestAStartedSendIsCountedOnceWithTheLeadsItLeftOut(t *testing.T) {
	h := newHarness(func(d *Deps) { d.Balances = fakeBalances{micros: 3 * 62500} })
	ids := h.seed(6)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	h.world.lateRunning[ids[0]] = true
	h.metrics.skips = map[string]int{}

	if _, err := h.svc.Start(context.Background(), startRequest(review, 3)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := h.svc.Start(context.Background(), startRequest(review, 0)); err != nil {
		t.Fatalf("a second start: %v", err)
	}
	if got := h.metrics.runs["send_template|"+metrics.LeadActionStarted+"|"+metrics.LeadActionNoFailure]; got != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
	if h.metrics.durations["send_template|"+metrics.LeadActionStarted] != 1 {
		t.Fatalf("durations = %v", h.metrics.durations)
	}
	if h.metrics.skips["send_template|"+string(campaign.SkipAlreadyInRunningCampaign)] != 1 || h.metrics.skips["send_template|"+string(campaign.SkipOverCap)] != 2 {
		t.Fatalf("skips = %v", h.metrics.skips)
	}
}

func TestAStartThatLostTheRaceToAnotherStartCountsNothing(t *testing.T) {
	h := newHarness(nil)
	h.seed(4)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	h.start.lostTheRun = true

	if _, err := h.svc.Start(context.Background(), startRequest(review, 0)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := h.metrics.runs["send_template|"+metrics.LeadActionStarted+"|"+metrics.LeadActionNoFailure]; got != 0 {
		t.Fatalf("a start that dispatched nothing was counted: runs = %v", h.metrics.runs)
	}
	if h.metrics.durations["send_template|"+metrics.LeadActionStarted] != 0 {
		t.Fatalf("a start that dispatched nothing was timed: durations = %v", h.metrics.durations)
	}
}

func TestTheUnofficialSendIsCountedUnderItsAction(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)
	review, err := prepare(h, scoped(), leadaction.ActionSendUnofficial, unofficialParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := h.svc.Start(context.Background(), startRequest(review, 0)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if h.metrics.runs["send_unofficial|"+metrics.LeadActionPrepared+"|"+metrics.LeadActionNoFailure] != 1 ||
		h.metrics.runs["send_unofficial|"+metrics.LeadActionStarted+"|"+metrics.LeadActionNoFailure] != 1 {
		t.Fatalf("runs = %v", h.metrics.runs)
	}
}
