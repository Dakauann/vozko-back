package carriercampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
)

func scheduledCampaign(id string, at time.Time) cc.Campaign {
	c := rcsCampaign(id)
	c.Status = cc.StatusScheduled
	c.ScheduledAt = &at
	return c
}

func TestRunSchedules(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	due := testNow.Add(-time.Minute)
	h.campaigns.put(scheduledCampaign("c1", due))
	h.campaigns.put(scheduledCampaign("c2", testNow.Add(time.Hour)))

	report, err := h.svc.RunSchedules(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Started) != 1 || report.Started[0] != "c1" || len(report.Failed) != 0 {
		t.Fatalf("report %+v", report)
	}
	if h.campaigns.get("c1").Status != cc.StatusRunning || h.campaigns.get("c2").Status != cc.StatusScheduled {
		t.Fatal("only the due campaign starts")
	}
	runs := h.runs.all()
	if len(runs) != 1 || !runs[0].Occurrence.Equal(due) || runs[0].Status != campaign.StatusRunning {
		t.Fatalf("runs %+v", runs)
	}
	if len(h.enqueuer.messages) != 1 {
		t.Fatalf("enqueued %d", len(h.enqueuer.messages))
	}
	if h.selections.scopes[0].ActorID != owner || h.selections.scopes[0].WorkspaceID != wsID {
		t.Fatalf("scope %+v", h.selections.scopes[0])
	}
}

func TestRunSchedulesFailureFallsBackToDraft(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.balance.micros = 0
	h.campaigns.put(scheduledCampaign("c1", testNow.Add(-time.Minute)))

	report, err := h.svc.RunSchedules(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Started) != 0 || !errors.Is(report.Failed["c1"], carriermessaging.ErrInsufficientBalance) {
		t.Fatalf("report %+v", report)
	}
	if stored := h.campaigns.get("c1"); stored.Status != cc.StatusDraft || stored.ScheduledAt != nil {
		t.Fatalf("stored %+v", stored)
	}
	if len(h.runs.all()) != 0 {
		t.Fatal("no run on failure")
	}
}

func TestRunSchedulesEmptyRecurringOccurrenceMovesOn(t *testing.T) {
	sp, _ := time.LoadLocation("America/Sao_Paulo")
	h := newHarness(t)
	due := time.Date(2026, 10, 21, 9, 0, 0, 0, sp)
	c := scheduledCampaign("c1", due)
	c.Recurrence = &cc.Recurrence{Frequency: cc.FrequencyDaily, Time: "09:00", Timezone: "America/Sao_Paulo"}
	h.campaigns.put(c)

	report, err := h.svc.RunSchedules(context.Background(), 10)
	if err != nil || len(report.Started) != 1 {
		t.Fatalf("report %+v err %v", report, err)
	}
	stored := h.campaigns.get("c1")
	if stored.Status != cc.StatusScheduled || stored.ScheduledAt == nil || !stored.ScheduledAt.Equal(time.Date(2026, 10, 22, 9, 0, 0, 0, sp)) {
		t.Fatalf("stored %+v", stored)
	}
	runs := h.runs.all()
	if len(runs) != 1 || runs[0].Status != campaign.StatusCompleted {
		t.Fatalf("runs %+v", runs)
	}
}

func TestRunSchedulesSkipsUnclaimed(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	c := scheduledCampaign("c1", testNow.Add(-time.Minute))
	h.campaigns.put(c)
	if _, err := h.campaigns.ClaimScheduledStart("c1", testNow); err != nil {
		t.Fatal(err)
	}
	c.ScheduledAt = ptr(testNow.Add(-time.Minute))
	report, err := h.svc.startDue(context.Background(), []*cc.Campaign{&c})
	if err != nil || len(report.Started) != 0 || len(report.Failed) != 0 {
		t.Fatalf("report %+v err %v", report, err)
	}
}
