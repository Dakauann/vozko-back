package carriercampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/rcs"
)

func TestPause(t *testing.T) {
	h := newHarness(t)
	h.runningRun(rcsCampaign("c1"), "run-1")
	if err := h.svc.Pause(context.Background(), wsID, "c1"); err != nil {
		t.Fatal(err)
	}
	if h.campaigns.get("c1").Status != cc.StatusPaused || h.runs.get("run-1").Status != campaign.StatusPaused || !h.queue.has("pause:run-1") {
		t.Fatalf("campaign %s run %s queue %v", h.campaigns.get("c1").Status, h.runs.get("run-1").Status, h.queue.calls)
	}
	if err := h.svc.Pause(context.Background(), wsID, "c1"); !errors.Is(err, campaign.ErrNotRunning) {
		t.Fatalf("second pause err = %v", err)
	}
}

func pausedRun(h *harness, c cc.Campaign) cc.Run {
	r := h.runningRun(c, "run-1")
	c.Status = cc.StatusPaused
	h.campaigns.put(c)
	r.Status = campaign.StatusPaused
	h.runs.put(r)
	return r
}

func TestResume(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := pausedRun(h, rcsCampaign("c1"))
	h.pendingRecipient(run, "rec-1", "lead-a", 1)
	if err := h.svc.Resume(context.Background(), wsID, "c1"); err != nil {
		t.Fatal(err)
	}
	if h.campaigns.get("c1").Status != cc.StatusRunning || h.runs.get("run-1").Status != campaign.StatusRunning {
		t.Fatalf("campaign %s run %s", h.campaigns.get("c1").Status, h.runs.get("run-1").Status)
	}
	for _, call := range []string{"resume:run-1", "subscribe:run-1", "seed:run-1"} {
		if !h.queue.has(call) {
			t.Fatalf("missing %s in %v", call, h.queue.calls)
		}
	}
}

func TestResumeRefusedWhileTheSenderIsPaused(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := pausedRun(h, rcsCampaign("c1"))
	h.pendingRecipient(run, "rec-1", "lead-a", 1)
	h.senders["sender-1"].Status = rcs.SenderPaused
	if err := h.svc.Resume(context.Background(), wsID, "c1"); !errors.Is(err, rcs.ErrSenderCannotSend) {
		t.Fatalf("err = %v", err)
	}
	if h.campaigns.get("c1").Status != cc.StatusPaused || h.runs.get("run-1").Status != campaign.StatusPaused {
		t.Fatal("a refused resume must change nothing")
	}
}

func TestResumeWithNothingPendingCompletes(t *testing.T) {
	h := newHarness(t)
	pausedRun(h, rcsCampaign("c1"))
	if err := h.svc.Resume(context.Background(), wsID, "c1"); err != nil {
		t.Fatal(err)
	}
	if h.runs.get("run-1").Status != campaign.StatusCompleted || h.campaigns.get("c1").Status != cc.StatusCompleted {
		t.Fatalf("run %s campaign %s", h.runs.get("run-1").Status, h.campaigns.get("c1").Status)
	}
}

func TestStop(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		h := newHarness(t)
		h.runningRun(rcsCampaign("c1"), "run-1")
		if err := h.svc.Stop(context.Background(), wsID, "c1"); err != nil {
			t.Fatal(err)
		}
		if h.campaigns.get("c1").Status != cc.StatusStopped || h.runs.get("run-1").Status != campaign.StatusStopped || !h.queue.has("stop:run-1") {
			t.Fatalf("campaign %s run %s", h.campaigns.get("c1").Status, h.runs.get("run-1").Status)
		}
	})
	t.Run("paused", func(t *testing.T) {
		h := newHarness(t)
		pausedRun(h, rcsCampaign("c1"))
		if err := h.svc.Stop(context.Background(), wsID, "c1"); err != nil {
			t.Fatal(err)
		}
		if h.campaigns.get("c1").Status != cc.StatusStopped || h.runs.get("run-1").Status != campaign.StatusStopped {
			t.Fatal("paused campaign should stop")
		}
	})
	t.Run("scheduled", func(t *testing.T) {
		h := newHarness(t)
		c := rcsCampaign("c1")
		c.Status = cc.StatusScheduled
		at := testNow.Add(time.Hour)
		c.ScheduledAt = &at
		h.campaigns.put(c)
		if err := h.svc.Stop(context.Background(), wsID, "c1"); err != nil {
			t.Fatal(err)
		}
		if stored := h.campaigns.get("c1"); stored.Status != cc.StatusStopped || stored.ScheduledAt != nil || len(h.queue.calls) != 0 {
			t.Fatalf("stored %+v calls %v", stored, h.queue.calls)
		}
	})
	t.Run("draft", func(t *testing.T) {
		h := newHarness(t)
		h.campaigns.put(rcsCampaign("c1"))
		if err := h.svc.Stop(context.Background(), wsID, "c1"); !errors.Is(err, cc.ErrNotLaunched) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestQueueStatusStore(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := h.runningRun(rcsCampaign("c1"), "run-1")
	h.pendingRecipient(run, "rec-1", "lead-a", 1)
	h.pendingRecipient(run, "rec-2", "lead-a", 1)
	ids, err := h.svc.ListRunningCampaignIDs()
	if err != nil || len(ids) != 1 || ids[0] != "run-1" {
		t.Fatalf("running %v %v", ids, err)
	}
	if n, err := h.svc.CountPendingEntries("run-1"); err != nil || n != 2 {
		t.Fatalf("pending %d %v", n, err)
	}
}

func TestCompleteCampaign(t *testing.T) {
	sp, _ := time.LoadLocation("America/Sao_Paulo")
	cases := []struct {
		name       string
		c          func() cc.Campaign
		funnel     cc.Funnel
		wantStatus cc.Status
		wantNext   *time.Time
		pauses     bool
	}{
		{"one off completes", func() cc.Campaign { return rcsCampaign("c1") }, cc.Funnel{}, cc.StatusCompleted, nil, false},
		{"recurring waits for the next occurrence", func() cc.Campaign {
			c := rcsCampaign("c1")
			c.Recurrence = &cc.Recurrence{Frequency: cc.FrequencyDaily, Time: "09:00", Timezone: "America/Sao_Paulo"}
			return c
		}, cc.Funnel{}, cc.StatusScheduled, ptr(time.Date(2026, 10, 22, 9, 0, 0, 0, sp)), false},
		{"opt out rate pauses the sender", func() cc.Campaign { return rcsCampaign("c1") }, cc.Funnel{Delivered: 300, OptOuts: 7}, cc.StatusCompleted, nil, true},
		{"healthy run keeps the sender", func() cc.Campaign { return rcsCampaign("c1") }, cc.Funnel{Delivered: 300, OptOuts: 6}, cc.StatusCompleted, nil, false},
		{"sms has no sender to pause", func() cc.Campaign { return smsCampaign("c1") }, cc.Funnel{Delivered: 300, OptOuts: 50}, cc.StatusCompleted, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.funnels.funnel = tc.funnel
			h.runningRun(tc.c(), "run-1")
			done, err := h.svc.CompleteCampaign("run-1")
			if err != nil || !done {
				t.Fatalf("done %v err %v", done, err)
			}
			stored := h.campaigns.get("c1")
			if stored.Status != tc.wantStatus || (stored.ScheduledAt == nil) != (tc.wantNext == nil) || (tc.wantNext != nil && !stored.ScheduledAt.Equal(*tc.wantNext)) {
				t.Fatalf("campaign %s at %v", stored.Status, stored.ScheduledAt)
			}
			if h.runs.get("run-1").Status != campaign.StatusCompleted {
				t.Fatal("run should complete")
			}
			if (len(h.pauser.paused) == 1) != tc.pauses {
				t.Fatalf("paused senders %v", h.pauser.paused)
			}
			if tc.pauses && (h.pauser.paused[0] != "sender-1" || h.pauser.reason != rcs.PauseOptOutRate) {
				t.Fatalf("pause %v %s", h.pauser.paused, h.pauser.reason)
			}
			if tc.pauses && (len(h.funnels.queries) == 0 || h.funnels.queries[0].RunID != "run-1") {
				t.Fatalf("health read %+v", h.funnels.queries)
			}
		})
	}
}

func TestCompleteCampaignOnlyOnce(t *testing.T) {
	h := newHarness(t)
	run := h.runningRun(rcsCampaign("c1"), "run-1")
	run.Status = campaign.StatusPaused
	h.runs.put(run)
	done, err := h.svc.CompleteCampaign("run-1")
	if err != nil || done {
		t.Fatalf("done %v err %v", done, err)
	}
	if h.campaigns.get("c1").Status != cc.StatusRunning {
		t.Fatal("a run that did not complete must leave the campaign alone")
	}
}

func ptr[T any](v T) *T { return &v }
