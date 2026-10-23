package emailcampaign

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNextBatchUnits(t *testing.T) {
	cases := []struct {
		name    string
		pending int64
		sent    int64
		paid    int64
		want    int64
	}{
		{name: "the first batch of a large run is a full batch", pending: 2500, want: BillingBatchSize},
		{name: "the first batch of a small run covers only the run", pending: 300, want: 300},
		{name: "the second batch covers what the first left uncovered", pending: 1500, sent: 1000, paid: 1000, want: 1000},
		{name: "the last batch is the remainder", pending: 500, sent: 2000, paid: 2000, want: 500},
		{name: "recipients in flight are already covered", pending: 1200, sent: 800, paid: 1000, want: 1000},
		{name: "a run fully paid needs no batch", pending: 200, sent: 800, paid: 1000, want: 0},
		{name: "nothing pending needs no batch", pending: 0, sent: 1000, paid: 1000, want: 0},
		{name: "more paid than needed never goes negative", pending: 0, sent: 10, paid: 1000, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextBatchUnits(tc.pending, tc.sent, tc.paid); got != tc.want {
				t.Fatalf("NextBatchUnits(%d, %d, %d) = %d, want %d", tc.pending, tc.sent, tc.paid, got, tc.want)
			}
		})
	}
}

func TestFirstBatchUnits(t *testing.T) {
	for sendable, want := range map[int64]int64{0: 0, 1: 1, 999: 999, 1000: 1000, 1001: 1000, 1_000_000: 1000} {
		if got := FirstBatchUnits(sendable); got != want {
			t.Errorf("FirstBatchUnits(%d) = %d, want %d", sendable, got, want)
		}
	}
}

func TestNewChargeBatch(t *testing.T) {
	b, err := NewChargeBatch(3, 500, 583, 165)
	if err != nil {
		t.Fatal(err)
	}
	if b.No != 3 || b.Units != 500 || b.UnitMicros != 583 || b.UnitCostMicros != 165 || b.TotalMicros != 291_500 || b.Charged() || b.Refunded() {
		t.Fatalf("batch = %+v", b)
	}
	cases := []struct {
		name  string
		no    int
		units int64
		unit  int64
	}{
		{"batch numbers start at one", 0, 10, 583},
		{"a batch has units", 1, 0, 583},
		{"a batch never exceeds the batch size", 1, BillingBatchSize + 1, 583},
		{"a batch has a price", 1, 10, 0},
	}
	for _, tc := range cases {
		if _, err := NewChargeBatch(tc.no, tc.units, tc.unit, 1); !errors.Is(err, ErrBatchInvalid) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestSlotCountersMaySend(t *testing.T) {
	cases := []struct {
		slots SlotCounters
		want  bool
	}{
		{SlotCounters{Paid: 0, Used: 0}, false},
		{SlotCounters{Paid: 1000, Used: 999}, true},
		{SlotCounters{Paid: 1000, Used: 1000}, false},
		{SlotCounters{Paid: 1000, Used: 1001}, false},
	}
	for _, tc := range cases {
		if got := tc.slots.HasFree(); got != tc.want {
			t.Errorf("%+v.HasFree() = %v, want %v", tc.slots, got, tc.want)
		}
	}
}

func charged(no int, units, unit int64) ChargeBatch {
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	return ChargeBatch{No: no, Units: units, UnitMicros: unit, UnitCostMicros: 100, TotalMicros: units * unit, ChargedAt: &at}
}

func TestPlanRefunds(t *testing.T) {
	open := ChargeBatch{No: 4, Units: 1000, UnitMicros: 700, TotalMicros: 700_000}
	cases := []struct {
		name    string
		batches []ChargeBatch
		unsent  int64
		want    []BatchRefund
	}{
		{name: "nothing unsent refunds nothing", batches: []ChargeBatch{charged(1, 1000, 583)}, unsent: 0, want: nil},
		{
			name: "the newest batch is refunded first at its own price", batches: []ChargeBatch{charged(1, 1000, 583), charged(2, 1000, 583)}, unsent: 600,
			want: []BatchRefund{{BatchNo: 2, Units: 600, UnitMicros: 583, UnitCostMicros: 100, Micros: 349_800}},
		},
		{
			name: "a refund larger than the newest batch continues into the older one", batches: []ChargeBatch{charged(1, 1000, 583), charged(2, 500, 700)}, unsent: 800,
			want: []BatchRefund{{BatchNo: 2, Units: 500, UnitMicros: 700, UnitCostMicros: 100, Micros: 350_000}, {BatchNo: 1, Units: 300, UnitMicros: 583, UnitCostMicros: 100, Micros: 174_900}},
		},
		{
			name: "never more than what the batches hold", batches: []ChargeBatch{charged(1, 200, 583)}, unsent: 5000,
			want: []BatchRefund{{BatchNo: 1, Units: 200, UnitMicros: 583, UnitCostMicros: 100, Micros: 116_600}},
		},
		{
			name: "a batch that was never charged is never refunded", batches: []ChargeBatch{charged(1, 1000, 583), charged(2, 1000, 583), charged(3, 1000, 583), open}, unsent: 100,
			want: []BatchRefund{{BatchNo: 3, Units: 100, UnitMicros: 583, UnitCostMicros: 100, Micros: 58_300}},
		},
		{name: "negative unsent refunds nothing", batches: []ChargeBatch{charged(1, 1000, 583)}, unsent: -3, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlanRefunds(tc.batches, tc.unsent); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("PlanRefunds = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBillingStateBatches(t *testing.T) {
	open := ChargeBatch{No: 3, Units: 500, UnitMicros: 583, TotalMicros: 291_500}
	state := BillingState{Slots: SlotCounters{Paid: 2000, Used: 1400}, Batches: []ChargeBatch{charged(1, 1000, 583), charged(2, 1000, 600), open}}
	if got := state.OpenBatch(); got == nil || got.No != 3 {
		t.Fatalf("open batch = %+v", got)
	}
	if got := state.NextBatchNo(); got != 4 {
		t.Fatalf("next batch = %d", got)
	}
	if got := state.CurrentBatch(); got != 2 {
		t.Fatalf("current batch = %d, want the last charged one", got)
	}
	if units, micros := state.Charged(); units != 2000 || micros != 1_183_000 {
		t.Fatalf("charged = %d, %d", units, micros)
	}
	empty := BillingState{}
	if empty.OpenBatch() != nil || empty.NextBatchNo() != 1 || empty.CurrentBatch() != 0 {
		t.Fatalf("empty state = %+v", empty)
	}
}
