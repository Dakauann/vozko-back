package carriercampaign_usecase

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

func TestLaunchSchedules(t *testing.T) {
	sp, _ := time.LoadLocation("America/Sao_Paulo")
	future := testNow.Add(3 * time.Hour)
	cases := []struct {
		name   string
		mutate func(c *cc.Campaign)
		want   time.Time
	}{
		{"one off", func(c *cc.Campaign) { c.ScheduledAt = &future }, future},
		{"recurring", func(c *cc.Campaign) {
			c.Recurrence = &cc.Recurrence{Frequency: cc.FrequencyDaily, Time: "09:00", Timezone: "America/Sao_Paulo"}
		}, time.Date(2026, 10, 22, 9, 0, 0, 0, sp)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addLead("lead-a", 1)
			c := rcsCampaign("c1")
			tc.mutate(&c)
			h.campaigns.put(c)
			got, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
			if err != nil {
				t.Fatal(err)
			}
			stored := h.campaigns.get("c1")
			if got.Run != nil || stored.Status != cc.StatusScheduled || stored.ScheduledAt == nil || !stored.ScheduledAt.Equal(tc.want) {
				t.Fatalf("launch %+v stored %+v", got, stored)
			}
			if len(h.runs.all()) != 0 || len(h.enqueuer.messages) != 0 {
				t.Fatal("scheduling must not start a run")
			}
		})
	}
}

func TestLaunchNow(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.addLead("lead-b", 2, func(l *lead.Lead) { l.Number = "" })
	h.addLead("lead-c", 1)
	h.addLead("lead-d", 4)
	h.addLead("lead-e", 5, func(l *lead.Lead) { l.Blocked = true })
	now := testNow
	h.addLead("lead-f", 6, func(l *lead.Lead) { l.OptedOutAt = &now })
	h.addLead("lead-g", 7)
	h.selections.ids = append(h.selections.ids, "lead-gone")
	h.suppressed[shared.NormalizePhone(phoneOf(4))] = true
	c := rcsCampaign("c1")
	c.Variants = []cc.Variant{
		{Name: "A", Content: rcs.Content{Text: "Oi {{1}}"}, Bindings: c.Variants[0].Bindings},
		{Name: "B", Content: rcs.Content{Text: "Olá {{1}}"}, Bindings: c.Variants[0].Bindings},
	}
	h.campaigns.put(c)

	got, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run == nil || got.Run.Status != campaign.StatusRunning || got.Run.CampaignID != "c1" || !got.Run.Occurrence.Equal(testNow) {
		t.Fatalf("run %+v", got.Run)
	}
	if stored := h.campaigns.get("c1"); stored.Status != cc.StatusRunning {
		t.Fatalf("campaign %s", stored.Status)
	}
	want := map[string]cc.SkipReason{
		"lead-a": "", "lead-b": cc.SkipNoPhone, "lead-c": cc.SkipDuplicate, "lead-d": cc.SkipSuppressed,
		"lead-e": cc.SkipBlocked, "lead-f": cc.SkipOptedOut, "lead-g": "", "lead-gone": cc.SkipNoPhone,
	}
	recipients := h.recipients.ofRun(got.Run.ID)
	if len(recipients) != len(want) {
		t.Fatalf("got %d recipients", len(recipients))
	}
	var pending []string
	for _, r := range recipients {
		reason, ok := want[r.LeadID]
		if !ok {
			t.Fatalf("unexpected recipient %+v", r)
		}
		if r.SkipReason != reason || (reason == "") != (r.Status == cc.RecipientPending) || (reason != "" && r.Status != cc.RecipientSkipped) {
			t.Fatalf("recipient %s: %+v", r.LeadID, r)
		}
		if r.WorkspaceID != wsID || r.CampaignID != "c1" || r.Variant != cc.PickVariant(r.ID, 2) {
			t.Fatalf("recipient fields %+v", r)
		}
		if r.Status == cc.RecipientPending {
			pending = append(pending, r.ID)
		}
	}
	if len(h.enqueuer.messages) != len(pending) || h.enqueuer.runID != got.Run.ID {
		t.Fatalf("enqueued %+v for %s", h.enqueuer.messages, h.enqueuer.runID)
	}
	for i, m := range h.enqueuer.messages {
		if m.CampaignID != got.Run.ID || m.EntryID != pending[i] {
			t.Fatalf("message %+v", m)
		}
	}
	if h.queue.calls[0] != "subscribe:"+got.Run.ID || h.queue.calls[1] != "enqueue:"+got.Run.ID {
		t.Fatalf("queue order %v", h.queue.calls)
	}
	if len(h.selections.dropped) != 1 || h.selections.dropped[0] != got.Run.ID {
		t.Fatalf("snapshot not dropped: %v", h.selections.dropped)
	}
}

func TestLaunchTransactionalKeepsOptedOutAndSuppressed(t *testing.T) {
	h := newHarness(t)
	now := testNow
	h.addLead("lead-a", 1, func(l *lead.Lead) { l.OptedOutAt = &now })
	h.addLead("lead-b", 2)
	h.suppressed[shared.NormalizePhone(phoneOf(2))] = true
	c := rcsCampaign("c1")
	c.Kind = cc.KindTransactional
	h.campaigns.put(c)
	got, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range h.recipients.ofRun(got.Run.ID) {
		if r.Status != cc.RecipientPending {
			t.Fatalf("transactional recipient skipped: %+v", r)
		}
	}
}

func TestLaunchPagesTheSnapshot(t *testing.T) {
	h := newHarness(t)
	for i := range 2*materializePage + 3 {
		h.addLead("lead-"+strconv.Itoa(10000+i), 100+i)
	}
	h.campaigns.put(rcsCampaign("c1"))
	got, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(h.recipients.ofRun(got.Run.ID)); n != 2*materializePage+3 {
		t.Fatalf("materialized %d", n)
	}
	if len(h.enqueuer.messages) != 2*materializePage+3 {
		t.Fatalf("enqueued %d", len(h.enqueuer.messages))
	}
}

func TestLaunchRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(h *harness, c *cc.Campaign)
		err    error
	}{
		{"shortfall", func(h *harness, c *cc.Campaign) { h.balance.micros = 10 }, carriermessaging.ErrInsufficientBalance},
		{"empty audience", func(h *harness, c *cc.Campaign) { h.selections.ids = nil }, ErrAudienceEmpty},
		{"paused sender", func(h *harness, c *cc.Campaign) { h.senders["sender-1"].Status = rcs.SenderPaused }, rcs.ErrSenderCannotSend},
		{"invalid campaign", func(h *harness, c *cc.Campaign) { c.Name = "" }, cc.ErrNameRequired},
		{"already running", func(h *harness, c *cc.Campaign) { c.Status = cc.StatusRunning }, cc.ErrNotEditable},
		{"audience grew since it was counted", func(h *harness, c *cc.Campaign) { c.Audience = everyone(5) }, selection.ErrCountChanged},
		{"audience never counted", func(h *harness, c *cc.Campaign) { c.Audience = everyone(0) }, selection.ErrEveryoneUnconfirmed},
		{"scheduled launch confirms the audience too", func(h *harness, c *cc.Campaign) {
			c.Audience = everyone(5)
			at := testNow.Add(time.Hour)
			c.ScheduledAt = &at
		}, selection.ErrCountChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addLead("lead-a", 1)
			c := rcsCampaign("c1")
			tc.mutate(h, &c)
			h.campaigns.put(c)
			_, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if h.campaigns.get("c1").Status != c.Status || len(h.runs.all()) != 0 || len(h.enqueuer.messages) != 0 {
				t.Fatal("a refused launch must change nothing")
			}
		})
	}
}

func TestLaunchShortfallCarriesTheMissingAmount(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.balance.micros = 0
	h.campaigns.put(rcsCampaign("c1"))
	_, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
	var shortfall *carriermessaging.ShortfallError
	if !errors.As(err, &shortfall) || shortfall.MissingMicros != 10_000+100*10_000 {
		t.Fatalf("err = %v", err)
	}
}

func TestLaunchWithEverySkippedCompletes(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1, func(l *lead.Lead) { l.Blocked = true })
	h.campaigns.put(rcsCampaign("c1"))
	got, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if h.runs.get(got.Run.ID).Status != campaign.StatusCompleted || h.campaigns.get("c1").Status != cc.StatusCompleted {
		t.Fatalf("run %s campaign %s", h.runs.get(got.Run.ID).Status, h.campaigns.get("c1").Status)
	}
	if len(h.enqueuer.messages) != 0 || h.queue.has("subscribe:"+got.Run.ID) {
		t.Fatal("nothing to enqueue")
	}
}

func TestLaunchFailureStopsTheRun(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.recipients.createErr = errBoom
	h.campaigns.put(rcsCampaign("c1"))
	if _, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner}); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v", err)
	}
	runs := h.runs.all()
	if len(runs) != 1 || runs[0].Status != campaign.StatusStopped || h.campaigns.get("c1").Status != cc.StatusDraft {
		t.Fatalf("runs %+v campaign %s", runs, h.campaigns.get("c1").Status)
	}
}

func everyone(expected int) selection.Selection {
	return selection.Selection{Mode: selection.ModeEveryone, Filter: &crmfilter.Filter{}, ExpectedCount: expected, Fingerprint: selection.Fingerprint(crmfilter.Filter{})}
}

func TestLaunchWithAConfirmedAudience(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	c := rcsCampaign("c1")
	c.Audience = everyone(1)
	h.campaigns.put(c)
	got, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil || got.Run == nil {
		t.Fatalf("Launch = %+v, %v", got, err)
	}
}
