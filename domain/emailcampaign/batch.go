package emailcampaign

import (
	"errors"
	"time"
)

const BillingBatchSize int64 = 1000

var (
	ErrBatchInvalid = errors.New("emailcampaign: a charge batch needs a number, units within the batch size and a price")
	ErrNoOpenBatch  = errors.New("emailcampaign: no batch is waiting to be charged")
)

type ChargeBatch struct {
	No             int
	Units          int64
	UnitMicros     int64
	UnitCostMicros int64
	TotalMicros    int64
	RefundedUnits  int64
	ChargedAt      *time.Time
	RefundedAt     *time.Time
}

func NewChargeBatch(no int, units, unitMicros, unitCostMicros int64) (ChargeBatch, error) {
	if no < 1 || units < 1 || units > BillingBatchSize || unitMicros <= 0 {
		return ChargeBatch{}, ErrBatchInvalid
	}
	return ChargeBatch{No: no, Units: units, UnitMicros: unitMicros, UnitCostMicros: unitCostMicros, TotalMicros: units * unitMicros}, nil
}

func (b ChargeBatch) Charged() bool { return b.ChargedAt != nil }

func (b ChargeBatch) Refunded() bool { return b.RefundedAt != nil }

func NextBatchUnits(pending, sent, paid int64) int64 {
	return min(BillingBatchSize, max(pending+sent-paid, 0))
}

func FirstBatchUnits(sendable int64) int64 {
	return NextBatchUnits(sendable, 0, 0)
}

type SlotCounters struct {
	Paid int64
	Used int64
}

func (s SlotCounters) HasFree() bool { return s.Used < s.Paid }

type BillingState struct {
	Slots   SlotCounters
	Batches []ChargeBatch
}

func (s BillingState) OpenBatch() *ChargeBatch {
	for i := range s.Batches {
		if !s.Batches[i].Charged() {
			b := s.Batches[i]
			return &b
		}
	}
	return nil
}

func (s BillingState) NextBatchNo() int {
	last := 0
	for _, b := range s.Batches {
		last = max(last, b.No)
	}
	return last + 1
}

func (s BillingState) CurrentBatch() int {
	current := 0
	for _, b := range s.Batches {
		if b.Charged() {
			current = max(current, b.No)
		}
	}
	return current
}

func (s BillingState) Charged() (int64, int64) {
	var units, micros int64
	for _, b := range s.Batches {
		if b.Charged() {
			units += b.Units
			micros += b.TotalMicros
		}
	}
	return units, micros
}

type BatchRefund struct {
	BatchNo        int
	Units          int64
	UnitMicros     int64
	UnitCostMicros int64
	Micros         int64
}

func PlanRefunds(batches []ChargeBatch, unsent int64) []BatchRefund {
	var out []BatchRefund
	left := max(unsent, 0)
	for i := len(batches) - 1; i >= 0 && left > 0; i-- {
		b := batches[i]
		if !b.Charged() {
			continue
		}
		units := min(left, b.Units)
		out = append(out, BatchRefund{BatchNo: b.No, Units: units, UnitMicros: b.UnitMicros, UnitCostMicros: b.UnitCostMicros, Micros: units * b.UnitMicros})
		left -= units
	}
	return out
}

func RefundTotals(refunds []BatchRefund) (int64, int64) {
	var units, micros int64
	for _, r := range refunds {
		units += r.Units
		micros += r.Micros
	}
	return units, micros
}
