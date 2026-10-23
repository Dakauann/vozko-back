package carriercampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/selection"
)

func TestReadyLeavesTheCampaignStartableWithoutSendingOrCharging(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.campaigns.put(rcsCampaign("c1"))
	got, err := h.svc.Ready(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != cc.StatusReady || h.campaigns.get("c1").Status != cc.StatusReady {
		t.Fatalf("got %s stored %s", got.Status, h.campaigns.get("c1").Status)
	}
	if len(h.runs.all()) != 0 || len(h.enqueuer.messages) != 0 || len(h.queue.calls) != 0 || len(h.selections.dropped) != 0 {
		t.Fatal("ready must not freeze, materialize or enqueue anything")
	}
}

func TestReadyRefusals(t *testing.T) {
	at := testNow.Add(time.Hour)
	cases := []struct {
		name   string
		mutate func(h *harness, c *cc.Campaign)
		err    error
	}{
		{"already ready", func(h *harness, c *cc.Campaign) { c.Status = cc.StatusReady }, cc.ErrNotDraft},
		{"scheduled", func(h *harness, c *cc.Campaign) { c.Status, c.ScheduledAt = cc.StatusScheduled, &at }, cc.ErrNotDraft},
		{"running", func(h *harness, c *cc.Campaign) { c.Status = cc.StatusRunning }, cc.ErrNotDraft},
		{"with a schedule", func(h *harness, c *cc.Campaign) { c.ScheduledAt = &at }, cc.ErrReadyScheduled},
		{"with a recurrence", func(h *harness, c *cc.Campaign) {
			c.Recurrence = &cc.Recurrence{Frequency: cc.FrequencyDaily, Time: "09:00", Timezone: "America/Sao_Paulo"}
		}, cc.ErrReadyScheduled},
		{"invalid campaign", func(h *harness, c *cc.Campaign) { c.Name = "" }, cc.ErrNameRequired},
		{"paused sender", func(h *harness, c *cc.Campaign) { h.senders["sender-1"].Status = rcs.SenderPaused }, rcs.ErrSenderCannotSend},
		{"audience grew since it was counted", func(h *harness, c *cc.Campaign) { c.Audience = everyone(5) }, selection.ErrCountChanged},
		{"empty audience", func(h *harness, c *cc.Campaign) { h.selections.ids = nil }, ErrAudienceEmpty},
		{"shortfall", func(h *harness, c *cc.Campaign) { h.balance.micros = 10 }, carriermessaging.ErrInsufficientBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addLead("lead-a", 1)
			c := rcsCampaign("c1")
			tc.mutate(h, &c)
			h.campaigns.put(c)
			if _, err := h.svc.Ready(context.Background(), wsID, "c1", Actor{UserID: owner}); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if h.campaigns.get("c1").Status != c.Status || len(h.runs.all()) != 0 || len(h.enqueuer.messages) != 0 {
				t.Fatal("a refused ready must change nothing")
			}
		})
	}
}

func TestReadyNeedsTheActorAndALaunchedChannel(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.campaigns.put(rcsCampaign("c1"))
	if _, err := h.svc.Ready(context.Background(), wsID, "c1", Actor{}); !errors.Is(err, ErrActorRequired) {
		t.Fatalf("no actor err = %v", err)
	}
	h.svc.launched = func(carriermessaging.Channel) bool { return false }
	if _, err := h.svc.Ready(context.Background(), wsID, "c1", Actor{UserID: owner}); !errors.Is(err, carriermessaging.ErrChannelNotLaunched) {
		t.Fatalf("not launched err = %v", err)
	}
	if h.campaigns.get("c1").Status != cc.StatusDraft {
		t.Fatal("a refused ready must change nothing")
	}
}

func TestReadyOfAnotherWorkspaceIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	if _, err := h.svc.Ready(context.Background(), "ws2", "c1", Actor{UserID: owner}); !errors.Is(err, cc.ErrCampaignNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadyCampaignLaunchesNow(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	c := rcsCampaign("c1")
	c.Status = cc.StatusReady
	h.campaigns.put(c)
	got, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run == nil || got.Run.Status != campaign.StatusRunning || h.campaigns.get("c1").Status != cc.StatusRunning {
		t.Fatalf("launch %+v stored %s", got, h.campaigns.get("c1").Status)
	}
}

func TestReadyCampaignSchedules(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	future := testNow.Add(3 * time.Hour)
	c := rcsCampaign("c1")
	c.Status, c.ScheduledAt = cc.StatusReady, &future
	h.campaigns.put(c)
	if _, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner}); err != nil {
		t.Fatal(err)
	}
	stored := h.campaigns.get("c1")
	if stored.Status != cc.StatusScheduled || stored.ScheduledAt == nil || !stored.ScheduledAt.Equal(future) || len(h.runs.all()) != 0 {
		t.Fatalf("stored %+v", stored)
	}
}

func TestReadyCampaignCannotStop(t *testing.T) {
	h := newHarness(t)
	c := rcsCampaign("c1")
	c.Status = cc.StatusReady
	h.campaigns.put(c)
	if err := h.svc.Stop(context.Background(), wsID, "c1"); !errors.Is(err, cc.ErrNotLaunched) {
		t.Fatalf("err = %v", err)
	}
	if h.campaigns.get("c1").Status != cc.StatusReady {
		t.Fatal("a refused stop must change nothing")
	}
}
