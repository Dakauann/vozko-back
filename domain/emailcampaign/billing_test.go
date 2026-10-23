package emailcampaign

import (
	"testing"
	"time"
)

func TestNewBilling(t *testing.T) {
	settledAt := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	twoBatches := BillingState{Slots: SlotCounters{Paid: 2000, Used: 1400}, Batches: []ChargeBatch{charged(1, 1000, 583), charged(2, 1000, 700)}}
	withOpen := BillingState{Slots: SlotCounters{Paid: 1000, Used: 1000}, Batches: []ChargeBatch{charged(1, 1000, 583), {No: 2, Units: 1000, UnitMicros: 583, TotalMicros: 583_000}}}
	cases := []struct {
		name       string
		state      BillingState
		sent       int64
		settlement *RunSettlement
		want       Billing
	}{
		{name: "nothing charged yet", state: BillingState{}, want: Billing{BatchSize: BillingBatchSize}},
		{
			name: "running estimates the refund of the unsent units newest batch first", state: twoBatches, sent: 1400,
			want: Billing{BatchSize: BillingBatchSize, CurrentBatch: 2, ChargedUnits: 2000, ChargedMicros: 1_283_000, SentUnits: 1400, FinalMicros: 1_283_000, RefundPendingMicros: 420_000},
		},
		{
			name: "a batch waiting for the wallet is not charged so far", state: withOpen, sent: 1000,
			want: Billing{BatchSize: BillingBatchSize, CurrentBatch: 1, ChargedUnits: 1000, ChargedMicros: 583_000, SentUnits: 1000, FinalMicros: 583_000},
		},
		{
			name: "settled shows the refund made", state: twoBatches, sent: 1400, settlement: &RunSettlement{RefundedUnits: 600, RefundedMicros: 420_000, SettledAt: settledAt},
			want: Billing{BatchSize: BillingBatchSize, CurrentBatch: 2, ChargedUnits: 2000, ChargedMicros: 1_283_000, SentUnits: 1400, RefundedUnits: 600, RefundedMicros: 420_000, FinalMicros: 863_000, SettledAt: &settledAt},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewBilling(tc.state, tc.sent, tc.settlement)
			if (got.SettledAt == nil) != (tc.want.SettledAt == nil) || (got.SettledAt != nil && !got.SettledAt.Equal(*tc.want.SettledAt)) {
				t.Fatalf("settled at = %v, want %v", got.SettledAt, tc.want.SettledAt)
			}
			got.SettledAt, tc.want.SettledAt = nil, nil
			if got != tc.want {
				t.Fatalf("billing = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHold(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	nextDay := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	if (Campaign{Status: StatusRunning}).Hold(now, "x") != nil {
		t.Fatal("no hold reason, no hold")
	}
	capped := Campaign{Status: StatusHeld, HoldReason: HoldDailyCap}.Hold(now, "")
	if capped == nil || capped.Reason != HoldDailyCap || capped.ResumeAt == nil || !capped.ResumeAt.Equal(nextDay) || capped.AccountStatusReason != "" {
		t.Fatalf("daily cap hold = %+v", capped)
	}
	paused := Campaign{Status: StatusHeld, HoldReason: HoldAccountPaused}.Hold(now, "bounce review")
	if paused == nil || paused.ResumeAt != nil || paused.AccountStatusReason != "bounce review" {
		t.Fatalf("paused hold = %+v", paused)
	}
	failed := Campaign{Status: StatusDraft, HoldReason: "launch_failed"}.Hold(now, "ignored")
	if failed == nil || failed.Reason != "launch_failed" || failed.ResumeAt != nil || failed.AccountStatusReason != "" {
		t.Fatalf("draft notice = %+v", failed)
	}
}

func TestListMetricsUseTheDetailRates(t *testing.T) {
	tally := Tally{Sent: 800, Delivered: 760, Opens: 380, Clicks: 95}
	got := NewListMetrics(tally)
	detail := NewMetrics(tally)
	want := ListMetrics{Sent: 800, Delivered: 760, DeliveryRate: detail.DeliveryRate, OpenRate: detail.OpenRate, ClickRate: detail.ClickRate}
	if got != want {
		t.Fatalf("list metrics = %+v, want %+v", got, want)
	}
}
