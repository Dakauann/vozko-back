package balance

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/billing"
	"vozko/domain/shared"
)

func TestMonthlySendCap_IsTheSharedMonthlyQuotaInBRT(t *testing.T) {
	cases := []struct {
		name string
		cap  MonthlySendCap
		want shared.MonthlyQuota
	}{
		{"cycle day kept", MonthlySendCap{Limit: 50, CycleDay: 15}, shared.MonthlyQuota{Limit: 50, CycleDay: 15, Location: billing.LocationBRT()}},
		{"missing cycle day is the first", MonthlySendCap{Limit: 50}, shared.MonthlyQuota{Limit: 50, CycleDay: 1, Location: billing.LocationBRT()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cap.Quota()
			if got.Limit != tc.want.Limit || got.CycleDay != tc.want.CycleDay || got.Location.String() != tc.want.Location.String() {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	c := MonthlySendCap{Limit: 2, CycleDay: 15}
	if !c.CycleStart(now).Equal(c.Quota().CycleStart(now)) || !c.NextCycleStart(now).Equal(c.Quota().NextCycleStart(now)) {
		t.Fatal("the send cap must count its cycle with the shared quota")
	}
	if err := c.CheckRoom(2); !errors.Is(err, ErrMonthlySendCapReached) || errors.Is(err, shared.ErrMonthlyQuotaReached) {
		t.Fatalf("the send cap keeps its own sentinel, got %v", err)
	}
}
