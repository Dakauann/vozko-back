package smartcampaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/working_hours"
)

func at(day, hour int) time.Time {
	return time.Date(2026, 10, day, hour, 0, 0, 0, brasilia)
}

func sizesAndTimes(waves []PlannedWave) ([]int, []time.Time) {
	sizes, times := make([]int, len(waves)), make([]time.Time, len(waves))
	for i, wave := range waves {
		sizes[i], times[i] = wave.Size, wave.At
	}
	return sizes, times
}

func TestAHealthyRunGrowsEveryObservationAndWaitsForTheNextOpening(t *testing.T) {
	plan, err := Project(standardPolicy(), 10000, at(16, 14))

	if err != nil || plan.Truncated {
		t.Fatalf("plan %+v err %v", plan, err)
	}
	sizes, times := sizesAndTimes(plan.Waves)
	wantSizes := []int{200, 400, 800, 1600, 3200, 3800}
	wantTimes := []time.Time{at(16, 14), at(16, 16), at(16, 18), at(17, 8), at(17, 10), at(17, 12)}
	for i := range wantSizes {
		if i >= len(sizes) || sizes[i] != wantSizes[i] || !times[i].Equal(wantTimes[i]) {
			t.Fatalf("waves %v at %v, want %v at %v", sizes, times, wantSizes, wantTimes)
		}
	}
	if len(sizes) != len(wantSizes) {
		t.Fatalf("got %d waves, want %d", len(sizes), len(wantSizes))
	}
}

func TestTheDailyLimitMovesTheNextWaveToTheNextDaysOpening(t *testing.T) {
	policy := standardPolicy()
	policy.MaxWavesPerDay = 2

	plan, err := Project(policy, 1500, at(16, 14))

	_, times := sizesAndTimes(plan.Waves)
	want := []time.Time{at(16, 14), at(16, 16), at(17, 8), at(17, 10)}
	if err != nil || len(times) != len(want) {
		t.Fatalf("times %v err %v", times, err)
	}
	for i := range want {
		if !times[i].Equal(want[i]) {
			t.Fatalf("times %v, want %v", times, want)
		}
	}
}

func TestAStartOutsideSendHoursWaitsForTheOpening(t *testing.T) {
	plan, err := Project(standardPolicy(), 100, time.Date(2026, 10, 18, 9, 0, 0, 0, brasilia))

	if err != nil || len(plan.Waves) != 1 || !plan.Waves[0].At.Equal(at(19, 8)) || plan.Waves[0].Size != 100 {
		t.Fatalf("plan %+v err %v, want one wave on Monday at the opening", plan, err)
	}
}

func TestAVeryLongRunIsCutAndSaysSo(t *testing.T) {
	policy := standardPolicy()
	policy.Growth, policy.FirstWaveSize, policy.MinWaveSize, policy.MaxWaveSize = 1, 50, 50, 50

	plan, err := Project(policy, 1_000_000, at(16, 14))

	if err != nil || !plan.Truncated || len(plan.Waves) != maxProjectedWaves {
		t.Fatalf("waves %d truncated %v err %v", len(plan.Waves), plan.Truncated, err)
	}
}

func TestSendHoursThatDoNotCompileAreRefused(t *testing.T) {
	policy := standardPolicy()
	policy.SendHours = &working_hours.Spec{Timezone: "Mars/Olympus", Days: businessHours().Days}

	if _, err := Project(policy, 100, at(16, 14)); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("err %v, want ErrInvalidPolicy", err)
	}
}
