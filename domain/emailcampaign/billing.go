package emailcampaign

import (
	"time"

	"vozko/domain/emailsending"
)

type RunSettlement struct {
	RefundedUnits  int64
	RefundedMicros int64
	SettledAt      time.Time
}

type Billing struct {
	BatchSize           int64
	CurrentBatch        int
	ChargedUnits        int64
	ChargedMicros       int64
	SentUnits           int64
	RefundedUnits       int64
	RefundedMicros      int64
	FinalMicros         int64
	SettledAt           *time.Time
	RefundPendingMicros int64
}

func NewBilling(state BillingState, sent int64, settlement *RunSettlement) Billing {
	b := Billing{BatchSize: BillingBatchSize, CurrentBatch: state.CurrentBatch(), SentUnits: sent}
	b.ChargedUnits, b.ChargedMicros = state.Charged()
	if settlement != nil {
		at := settlement.SettledAt
		b.RefundedUnits, b.RefundedMicros, b.SettledAt = settlement.RefundedUnits, settlement.RefundedMicros, &at
	} else {
		_, b.RefundPendingMicros = RefundTotals(PlanRefunds(state.Batches, b.ChargedUnits-sent))
	}
	b.FinalMicros = b.ChargedMicros - b.RefundedMicros
	return b
}

type Hold struct {
	Reason              HoldReason
	ResumeAt            *time.Time
	AccountStatusReason string
}

func (c Campaign) Hold(now time.Time, accountStatusReason string) *Hold {
	if c.HoldReason == HoldNone {
		return nil
	}
	h := &Hold{Reason: c.HoldReason}
	switch c.HoldReason {
	case HoldDailyCap:
		if c.Status == StatusHeld {
			at := emailsending.NextDayStart(now)
			h.ResumeAt = &at
		}
	case HoldAccountPaused:
		h.AccountStatusReason = accountStatusReason
	}
	return h
}

type ListMetrics struct {
	Sent         int64
	Delivered    int64
	DeliveryRate int
	OpenRate     int
	ClickRate    int
}

func NewListMetrics(t Tally) ListMetrics {
	m := NewMetrics(t)
	return ListMetrics{Sent: m.Sent, Delivered: m.Delivered, DeliveryRate: m.DeliveryRate, OpenRate: m.OpenRate, ClickRate: m.ClickRate}
}
