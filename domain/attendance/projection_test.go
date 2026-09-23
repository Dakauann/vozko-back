package attendance

import (
	"testing"
	"time"

	wh "vozko/domain/working_hours"
)

func testSchedule(t *testing.T) *wh.Schedule {
	t.Helper()
	spec := &wh.Spec{
		Timezone: "America/Sao_Paulo",
		Days: map[string][]wh.Window{
			"mon": {{Start: "09:00", End: "18:00"}},
			"tue": {{Start: "09:00", End: "18:00"}},
			"wed": {{Start: "09:00", End: "18:00"}},
			"thu": {{Start: "09:00", End: "18:00"}},
			"fri": {{Start: "09:00", End: "18:00"}},
		},
	}
	sched, err := spec.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return sched
}

func saoPauloTime(t *testing.T, value string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		t.Fatalf("ParseInLocation(%q) error = %v", value, err)
	}
	return parsed
}

func ptr(v float64) *float64 { return &v }

func TestBuildPeriodWithoutAScheduleIsUnavailable(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	period := BuildPeriod(nil, start, start.AddDate(0, 1, 0), start.AddDate(0, 0, 10))

	if period.Available {
		t.Fatalf("BuildPeriod() without a schedule Available = true, want false")
	}
	if period.Reason != ReasonNoSchedule {
		t.Fatalf("BuildPeriod() Reason = %q, want %q", period.Reason, ReasonNoSchedule)
	}
	if period.OpenDaysTotal != 0 {
		t.Fatalf("BuildPeriod() OpenDaysTotal = %d, want 0", period.OpenDaysTotal)
	}
}

func TestBuildPeriodMidMonth(t *testing.T) {
	sched := testSchedule(t)
	start := saoPauloTime(t, "2026-09-01 00:00")
	end := saoPauloTime(t, "2026-10-01 00:00")
	now := saoPauloTime(t, "2026-09-15 13:30")

	period := BuildPeriod(sched, start, end, now)
	if !period.Available {
		t.Fatalf("BuildPeriod() Available = false, want true")
	}
	if period.OpenDaysTotal != 22 {
		t.Fatalf("BuildPeriod() OpenDaysTotal = %d, want 22", period.OpenDaysTotal)
	}
	if period.OpenDaysDone != 11 {
		t.Fatalf("BuildPeriod() OpenDaysDone = %d, want 11", period.OpenDaysDone)
	}
	if period.OpenDaysLeft != 11 {
		t.Fatalf("BuildPeriod() OpenDaysLeft = %d, want 11", period.OpenDaysLeft)
	}
	if period.ElapsedPct <= 0 || period.ElapsedPct >= 100 {
		t.Fatalf("BuildPeriod() ElapsedPct = %v, want strictly between 0 and 100", period.ElapsedPct)
	}
}

func maturePeriod() Period {
	return Period{
		OpenDaysTotal: 21,
		OpenDaysDone:  19,
		OpenDaysLeft:  2,
		ElapsedPct:    90,
		Available:     true,
	}
}

func TestBuildProjectionNoTargetIsNeverOnTrack(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	got := BuildProjection(spec, 1538, true, nil, maturePeriod())

	if got.Verdict != VerdictNoTarget {
		t.Fatalf("BuildProjection() Verdict = %q, want %q", got.Verdict, VerdictNoTarget)
	}
	if got.Target != nil {
		t.Fatalf("BuildProjection() Target = %v, want nil", *got.Target)
	}
	if got.Projected == nil {
		t.Fatalf("BuildProjection() Projected = nil, want a run rate even without a target")
	}
}

func TestBuildProjectionHigherIsBetter(t *testing.T) {
	spec, _ := Metric(MetricFinished)

	onTrack := BuildProjection(spec, 1900, true, ptr(1786), maturePeriod())
	if onTrack.Verdict != VerdictOnTrack {
		t.Fatalf("BuildProjection() above target Verdict = %q, want %q", onTrack.Verdict, VerdictOnTrack)
	}

	offTrack := BuildProjection(spec, 1000, true, ptr(1786), maturePeriod())
	if offTrack.Verdict != VerdictOffTrack {
		t.Fatalf("BuildProjection() well below target Verdict = %q, want %q", offTrack.Verdict, VerdictOffTrack)
	}
}

func TestBuildProjectionAtRiskBand(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	period := Period{OpenDaysTotal: 10, OpenDaysDone: 10, OpenDaysLeft: 0, ElapsedPct: 100, Available: true}

	atRisk := BuildProjection(spec, 980, true, ptr(1000), period)
	if atRisk.Verdict != VerdictAtRisk {
		t.Fatalf("BuildProjection() inside the band Verdict = %q, want %q", atRisk.Verdict, VerdictAtRisk)
	}

	outside := BuildProjection(spec, 900, true, ptr(1000), period)
	if outside.Verdict != VerdictOffTrack {
		t.Fatalf("BuildProjection() outside the band Verdict = %q, want %q", outside.Verdict, VerdictOffTrack)
	}
}

func TestBuildProjectionLowerIsBetterInvertsTheVerdict(t *testing.T) {
	spec, _ := Metric(MetricAvgFRTMins)
	period := maturePeriod()

	fast := BuildProjection(spec, 3, true, ptr(5), period)
	if fast.Verdict != VerdictOnTrack {
		t.Fatalf("BuildProjection() under a lower-is-better target Verdict = %q, want %q", fast.Verdict, VerdictOnTrack)
	}
	if fast.AttainPct == nil || *fast.AttainPct <= 100 {
		t.Fatalf("BuildProjection() AttainPct = %v, want above 100 when beating a lower-is-better target", fast.AttainPct)
	}

	slow := BuildProjection(spec, 12, true, ptr(5), period)
	if slow.Verdict != VerdictOffTrack {
		t.Fatalf("BuildProjection() over a lower-is-better target Verdict = %q, want %q", slow.Verdict, VerdictOffTrack)
	}
}

func TestBuildProjectionNeverProjectsAnAverage(t *testing.T) {
	spec, _ := Metric(MetricAvgHandleMins)
	got := BuildProjection(spec, 22, true, ptr(20), maturePeriod())

	if got.Projected != nil {
		t.Fatalf("BuildProjection() Projected = %v, want nil for a non-cumulative metric", *got.Projected)
	}
	if got.PerOpenDay != nil {
		t.Fatalf("BuildProjection() PerOpenDay = %v, want nil for a non-cumulative metric", *got.PerOpenDay)
	}
	if got.Verdict != VerdictOffTrack {
		t.Fatalf("BuildProjection() Verdict = %q, want the actual compared to target", got.Verdict)
	}
}

func TestBuildProjectionTooEarlyToProject(t *testing.T) {
	spec, _ := Metric(MetricFinished)

	noDays := BuildProjection(spec, 10, true, ptr(100), Period{
		OpenDaysTotal: 21, OpenDaysDone: 0, ElapsedPct: 2, Available: true,
	})
	if noDays.Projected != nil || noDays.Reason != ReasonTooEarly {
		t.Fatalf("BuildProjection() with no open day done = %+v, want no projection and %q", noDays, ReasonTooEarly)
	}

	thinPct := BuildProjection(spec, 10, true, ptr(100), Period{
		OpenDaysTotal: 21, OpenDaysDone: 1, ElapsedPct: 4, Available: true,
	})
	if thinPct.Projected != nil || thinPct.Reason != ReasonTooEarly {
		t.Fatalf("BuildProjection() below the elapsed floor = %+v, want no projection and %q", thinPct, ReasonTooEarly)
	}
}

func TestBuildProjectionUnavailablePeriodCarriesItsReason(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	got := BuildProjection(spec, 10, true, ptr(100), Period{Reason: ReasonNoSchedule})

	if got.Verdict != VerdictNotProjected {
		t.Fatalf("BuildProjection() Verdict = %q, want %q", got.Verdict, VerdictNotProjected)
	}
	if got.Reason != ReasonNoSchedule {
		t.Fatalf("BuildProjection() Reason = %q, want %q", got.Reason, ReasonNoSchedule)
	}
}

func TestBuildProjectionUnknownActualIsUnavailable(t *testing.T) {
	spec, _ := Metric(MetricRevenueCents)
	got := BuildProjection(spec, 0, false, ptr(100), maturePeriod())

	if got.Available {
		t.Fatalf("BuildProjection() with an unknown actual Available = true, want false")
	}
	if got.Reason != ReasonNoActual {
		t.Fatalf("BuildProjection() Reason = %q, want %q", got.Reason, ReasonNoActual)
	}
}

func TestMetricActualResolutionPctNeedsAPopulation(t *testing.T) {
	empty := &Overview{}
	if _, known := MetricActual(empty, MetricResolutionPct); known {
		t.Fatalf("MetricActual(resolution_pct) on an empty overview known = true, want false")
	}

	populated := &Overview{KPIs: OverviewKPIs{Finished: 30, Ongoing: 10, Pending: 10}}
	value, known := MetricActual(populated, MetricResolutionPct)
	if !known || value != 60 {
		t.Fatalf("MetricActual(resolution_pct) = %v, %v, want 60, true", value, known)
	}
}

func TestMetricActualRevenueRefusesMixedCurrencies(t *testing.T) {
	mixed := &Overview{Revenue: Revenue{
		Available:       true,
		MixedCurrencies: true,
		Currencies: []RevenueByCurrency{
			{Currency: "BRL", ValueCents: 100},
			{Currency: "USD", ValueCents: 200},
		},
	}}
	if _, known := MetricActual(mixed, MetricRevenueCents); known {
		t.Fatalf("MetricActual(revenue_cents) across currencies known = true, want false")
	}
}

func TestTargetableMetricsAreStableAndResolvable(t *testing.T) {
	metrics := TargetableMetrics()
	if len(metrics) == 0 {
		t.Fatalf("TargetableMetrics() is empty")
	}
	for i, spec := range metrics {
		if i > 0 {
			previous := metrics[i-1]
			sameCategory := previous.Category == spec.Category
			if sameCategory && previous.Key >= spec.Key {
				t.Fatalf("TargetableMetrics() is not sorted inside %q at %d: %q then %q",
					spec.Category, i, previous.Key, spec.Key)
			}
			if !sameCategory && previous.Category.Order() > spec.Category.Order() {
				t.Fatalf("TargetableMetrics() categories are out of order at %d: %q then %q",
					i, previous.Category, spec.Category)
			}
		}
		if !spec.Kind.Valid() {
			t.Fatalf("TargetableMetrics() %q has an invalid kind %q", spec.Key, spec.Kind)
		}
		if _, found := Metric(spec.Key); !found {
			t.Fatalf("TargetableMetrics() returned %q which Metric() cannot resolve", spec.Key)
		}
	}
}
