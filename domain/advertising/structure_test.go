package advertising

import (
	"errors"
	"testing"
	"time"
)

func TestDeliveryReadsLikeMetaAdsManager(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	cases := []struct {
		status EffectiveStatus
		start  *time.Time
		end    *time.Time
		want   Delivery
	}{
		{EffectiveActive, nil, nil, DeliveryActive},
		{EffectiveActive, &future, nil, DeliveryScheduled},
		{EffectiveActive, nil, &past, DeliveryCompleted},
		{EffectivePaused, nil, nil, DeliveryOff},
		{EffectiveCampaignPaused, nil, nil, DeliveryCampaignOff},
		{EffectiveAdSetPaused, nil, nil, DeliveryAdSetOff},
		{EffectivePendingReview, nil, nil, DeliveryInReview},
		{EffectiveInProcess, nil, nil, DeliveryInReview},
		{EffectiveDisapproved, nil, nil, DeliveryRejected},
		{EffectiveWithIssues, nil, nil, DeliveryWithIssues},
		{EffectivePendingBilling, nil, nil, DeliveryPendingBilling},
		{EffectiveArchived, nil, nil, DeliveryArchived},
		{EffectiveDeleted, nil, nil, DeliveryDeleted},
		{"SOMETHING_NEW", nil, nil, DeliveryUnknown},
	}
	for _, c := range cases {
		o := &Object{EffectiveStatus: c.status, StartTime: c.start, EndTime: c.end, FirstDeliveredAt: &past}
		if got := o.Delivery(now); got != c.want {
			t.Fatalf("%s: got %s, want %s", c.status, got, c.want)
		}
	}
}

func TestUnknownStatusIsNeverShownAsActive(t *testing.T) {
	o := &Object{EffectiveStatus: ""}
	if o.Delivery(time.Now()) == DeliveryActive {
		t.Fatal("empty status read as active")
	}
}

func TestDeletedOrArchivedObjectsCannotBeToggled(t *testing.T) {
	for _, o := range []*Object{
		{Status: StatusDeleted}, {Status: StatusArchived},
		{Status: StatusPaused, EffectiveStatus: EffectiveArchived},
	} {
		if err := o.CanToggle(); !errors.Is(err, ErrObjectLocked) {
			t.Fatalf("%+v toggled: %v", o, err)
		}
	}
	if err := (&Object{Status: StatusPaused, EffectiveStatus: EffectiveDisapproved}).CanToggle(); err != nil {
		t.Fatalf("rejected ad cannot be paused: %v", err)
	}
}

func TestMetaAllowsFourBudgetChangesPerHour(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	o := &Object{Level: LevelAdSet, DailyBudget: 2000}
	for i := 0; i < 4; i++ {
		if err := o.CanChangeBudget(now); err != nil {
			t.Fatalf("change %d refused: %v", i+1, err)
		}
		o.RecordBudgetChange(now.Add(time.Duration(i) * time.Minute))
	}
	if err := o.CanChangeBudget(now.Add(5 * time.Minute)); !errors.Is(err, ErrBudgetChangeTooSoon) {
		t.Fatalf("fifth change in the hour: %v", err)
	}
	if err := o.CanChangeBudget(now.Add(61 * time.Minute)); err != nil {
		t.Fatalf("change after the window refused: %v", err)
	}
}

func TestBudgetChangeHistoryForgetsOldEntries(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	o := &Object{BudgetChanges: []time.Time{now.Add(-3 * time.Hour), now.Add(-2 * time.Hour)}}
	o.RecordBudgetChange(now)
	if len(o.BudgetChanges) != 1 {
		t.Fatalf("history kept %d entries", len(o.BudgetChanges))
	}
}

func TestOnlyObjectsThatCarryABudgetCanChangeIt(t *testing.T) {
	now := time.Now()
	if err := (&Object{Level: LevelAdSet, LifetimeBudget: 5000}).CanChangeBudget(now); err != nil {
		t.Fatalf("lifetime budget refused: %v", err)
	}
	for _, o := range []*Object{{Level: LevelAd, DailyBudget: 100}, {Level: LevelCampaign}} {
		if err := o.CanChangeBudget(now); !errors.Is(err, ErrNoBudget) {
			t.Fatalf("%+v: %v", o, err)
		}
	}
	if err := ValidateDailyBudget(0); !errors.Is(err, ErrInvalidBudget) {
		t.Fatal("zero budget accepted")
	}
}
