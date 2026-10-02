package advertising

import (
	"errors"
	"testing"
)

func TestOnlyMetaBreakdownCombinationsAreAccepted(t *testing.T) {
	for _, ok := range [][]Breakdown{nil, {BreakdownAge}, {BreakdownGender, BreakdownAge}, {BreakdownPosition, BreakdownPlatform}} {
		if err := ValidateBreakdowns(ok); err != nil {
			t.Fatalf("%v refused: %v", ok, err)
		}
	}
	for _, bad := range [][]Breakdown{{BreakdownAge, BreakdownCountry}, {BreakdownPosition}, {"weather"}} {
		if err := ValidateBreakdowns(bad); !errors.Is(err, ErrBreakdownCombination) {
			t.Fatalf("%v accepted", bad)
		}
	}
}

func TestRemovedViewWindowsAreRefusedBecauseMetaReturnsEmptyData(t *testing.T) {
	if err := ValidateWindows([]AttributionWindow{"7d_view"}); err == nil {
		t.Fatal("7d_view accepted")
	}
	if err := ValidateWindows([]AttributionWindow{Window7DayClick, Window1DayView}); err != nil {
		t.Fatal(err)
	}
}

func TestPreviousRangeHasTheSameLengthRightBefore(t *testing.T) {
	r, _ := NewDateRange("2026-09-24", "2026-09-30")
	prev := PreviousRange(r)
	if prev.Since.Format(DayLayout) != "2026-09-17" || prev.Until.Format(DayLayout) != "2026-09-23" || prev.Days() != 7 {
		t.Fatalf("previous %s %s", prev.Since.Format(DayLayout), prev.Until.Format(DayLayout))
	}
}

func TestPercentChangeIsUnknownFromZero(t *testing.T) {
	if PercentChange(10, 0) != nil || *PercentChange(15, 10) != 50 {
		t.Fatal("percent change wrong")
	}
}

func TestCostPerThruPlay(t *testing.T) {
	m := LiveMetrics{Metrics: Metrics{SpendMicros: 10_000_000}, Video: VideoMetrics{ThruPlays: 4}}
	if *m.CostPerThruPlay() != 2_500_000 {
		t.Fatalf("cost %d", *m.CostPerThruPlay())
	}
}
