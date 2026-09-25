package attendance

import (
	"errors"
	"testing"
	"time"
)

var digestToday = time.Date(2026, 9, 25, 15, 30, 0, 0, time.UTC)

func TestParseWindow(t *testing.T) {
	day := func(s string) time.Time {
		d, _ := time.Parse(DayLayout, s)
		return d
	}
	cases := []struct {
		name     string
		from, to string
		wantFrom time.Time
		wantTo   time.Time
		wantErr  bool
	}{
		// "How are we doing?" names no period; the default must be a real, bounded one.
		{"no dates means the last 30 days", "", "", day("2026-08-27"), EndOfDay(day("2026-09-25")), false},
		{"only a start runs to today", "2026-09-01", "", day("2026-09-01"), EndOfDay(day("2026-09-25")), false},
		{"only an end looks back 30 days from it", "", "2026-06-30", day("2026-06-01"), EndOfDay(day("2026-06-30")), false},
		{"a single day", "2026-09-10", "2026-09-10", day("2026-09-10"), EndOfDay(day("2026-09-10")), false},
		{"a full year fits", "2025-09-25", "2026-09-25", day("2025-09-25"), EndOfDay(day("2026-09-25")), false},
		// One day past a year: the query cost grows with the span, so the ceiling is enforced, not advised.
		{"more than a year is refused", "2025-09-24", "2026-09-25", time.Time{}, time.Time{}, true},
		{"reversed range is refused", "2026-09-10", "2026-09-01", time.Time{}, time.Time{}, true},
		{"a start in the future is refused", "2026-10-01", "2026-10-05", time.Time{}, time.Time{}, true},
		{"a malformed date is refused, not ignored", "01/09/2026", "", time.Time{}, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := ParseWindow(tc.from, tc.to, digestToday)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidWindow) {
					t.Fatalf("err = %v, want ErrInvalidWindow", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !w.From.Equal(tc.wantFrom) || !w.To.Equal(tc.wantTo) {
				t.Fatalf("window = %v..%v, want %v..%v", w.From, w.To, tc.wantFrom, tc.wantTo)
			}
		})
	}
}

func TestWindowAppliesTheSameDayConventionAsThePage(t *testing.T) {
	w, _ := ParseWindow("2026-09-01", "2026-09-07", digestToday)
	var f OverviewFilter
	w.Apply(&f)
	if f.DateFrom == nil || f.DateTo == nil {
		t.Fatal("Apply left the filter open-ended")
	}
	if got := f.DateTo.Format(time.RFC3339); got != "2026-09-07T23:59:59Z" {
		t.Fatalf("DateTo = %s, want the last second of the day like the section handler", got)
	}
	if w.Days() != 7 {
		t.Fatalf("Days() = %d, want 7", w.Days())
	}
}

func summaryFixture() *SummarySection {
	frt := 12.345
	return &SummarySection{
		KPIs: OverviewKPIs{Finished: 80, Ongoing: 15, Pending: 5, AvgFRTMins: &frt},
		Projections: []MetricProjection{
			{MetricKey: MetricFinished, Target: ptr(100), Projected: ptr(96.4), Verdict: VerdictAtRisk},
			{MetricKey: MetricAvgFRTMins, Verdict: VerdictNoTarget},
		},
	}
}

func TestReadMetricsReturnsOnlyWhatWasAsked(t *testing.T) {
	got, err := ReadMetrics(summaryFixture(), []string{MetricFinished, MetricAvgFRTMins, MetricResolutionPct, MetricFinished})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (duplicates collapse)", len(got))
	}
	finished := got[0]
	if finished.Value == nil || *finished.Value != 80 || finished.Target == nil || *finished.Target != 100 || finished.Verdict != VerdictAtRisk {
		t.Fatalf("finished = %+v", finished)
	}
	if got[1].Value == nil || *got[1].Value != 12.35 {
		t.Fatalf("frt = %v, want rounded 12.35", got[1].Value)
	}
	// A projection without a target is not a verdict worth repeating to the user.
	if got[1].Verdict != "" || got[1].Target != nil {
		t.Fatalf("frt carries a verdict without a target: %+v", got[1])
	}
	if got[2].Value == nil || *got[2].Value != 80 {
		t.Fatalf("resolution = %v, want 80", got[2].Value)
	}
}

func TestReadMetricsReportsMissingValuesAsNullNotZero(t *testing.T) {
	got, err := ReadMetrics(summaryFixture(), []string{MetricAvgWaitMins})
	if err != nil {
		t.Fatal(err)
	}
	// A zero wait time is good news; an unmeasured one is not. The model must see the difference.
	if got[0].Value != nil {
		t.Fatalf("value = %v, want nil", *got[0].Value)
	}
}

func TestReadMetricsDefaultsToTheWholeCatalogue(t *testing.T) {
	got, err := ReadMetrics(summaryFixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(MetricKeys()) {
		t.Fatalf("len = %d, want %d", len(got), len(MetricKeys()))
	}
}

func TestReadMetricsRefusesUnknownKeys(t *testing.T) {
	if _, err := ReadMetrics(summaryFixture(), []string{"nps"}); !errors.Is(err, ErrUnknownMetric) {
		t.Fatalf("err = %v, want ErrUnknownMetric", err)
	}
}

func rankedFixture() TeamRanking {
	member := func(name string, value float64) RankedMember {
		return RankedMember{MemberRow: MemberRow{ActorID: name, DisplayName: name, Email: name + "@x.com"}, RankMetricValue: value}
	}
	return TeamRanking{
		RankMetricKey: "resolved",
		TeamAverage:   ptr(20),
		Members:       []RankedMember{member("a", 40), member("b", 30), member("c", 20), member("d", 10), member("e", 0)},
		Available:     true,
	}
}

func TestDigestTeamTakesTheTopOrTheBottom(t *testing.T) {
	top := DigestTeam(rankedFixture(), 2, false)
	if len(top.Members) != 2 || top.Members[0].Name != "a" || top.Members[1].Name != "b" {
		t.Fatalf("top = %+v", top.Members)
	}
	bottom := DigestTeam(rankedFixture(), 2, true)
	if len(bottom.Members) != 2 || bottom.Members[0].Name != "e" || bottom.Members[1].Name != "d" {
		t.Fatalf("bottom = %+v", bottom.Members)
	}
	if top.TotalMembers != 5 {
		t.Fatalf("TotalMembers = %d, want 5", top.TotalMembers)
	}
}

func TestDigestTeamCapsTheListAndDropsEmails(t *testing.T) {
	d := DigestTeam(rankedFixture(), 500, false)
	if len(d.Members) != 5 {
		t.Fatalf("len = %d", len(d.Members))
	}
	many := rankedFixture()
	for i := 0; i < 40; i++ {
		many.Members = append(many.Members, many.Members[0])
	}
	if got := len(DigestTeam(many, 500, false).Members); got != MaxAssistantListItems {
		t.Fatalf("len = %d, want the cap %d", got, MaxAssistantListItems)
	}
	if got := len(DigestTeam(many, 0, false).Members); got != DefaultAssistantListItems {
		t.Fatalf("len = %d, want the default %d", got, DefaultAssistantListItems)
	}
}

func TestDigestTrendKeepsOnlyRequestedSeries(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	trend := Trend{Available: true, Series: []TrendSeries{
		BuildTrend(spec, []TrendPoint{closedPoint("2026-07", 10), closedPoint("2026-08", 12)}, nil),
		{MetricKey: MetricAvgFRTMins, Available: true},
	}}
	got, err := DigestTrend(trend, []string{MetricFinished})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Metric != MetricFinished || len(got[0].Points) != 2 {
		t.Fatalf("got = %+v", got)
	}
	if _, err := DigestTrend(trend, nil); !errors.Is(err, ErrUnknownMetric) {
		t.Fatalf("err = %v, want ErrUnknownMetric when no metric is named", err)
	}
	if _, err := DigestTrend(trend, []string{"nps"}); !errors.Is(err, ErrUnknownMetric) {
		t.Fatalf("err = %v, want ErrUnknownMetric", err)
	}
}

func TestDigestBacklogKeepsTheOldestAgeBucket(t *testing.T) {
	buckets := make([]XrayBucket, 0, 8)
	for i := 0; i < 8; i++ {
		buckets = append(buckets, XrayBucket{Key: string(rune('a' + i)), Count: 1})
	}
	x := BacklogXray{
		Total:     8,
		Available: true,
		Age:       XrayDimension{Dimension: "age", Buckets: buckets, Available: true},
		Origin:    XrayDimension{Dimension: "origin"},
	}
	got := DigestBacklog(x)
	if got.Total != 8 || len(got.Dimensions) != 1 {
		t.Fatalf("got = %+v, want only the available dimension", got)
	}
	// Age is an ordered dimension: its tail is the stalest backlog, the one a manager asks about.
	if last := got.Dimensions[0].Buckets; len(last) != 8 || last[7].Key != "h" {
		t.Fatalf("age buckets = %+v, want all eight in order", last)
	}
}

func TestPreviousWindowIsTheSameLengthRightBefore(t *testing.T) {
	w, _ := ParseWindow("2026-09-01", "2026-09-07", digestToday)
	prev := w.Previous()
	if prev.From.Format(DayLayout) != "2026-08-25" || prev.To.Format(time.RFC3339) != "2026-08-31T23:59:59Z" {
		t.Fatalf("previous = %v..%v", prev.From, prev.To)
	}
	if prev.Days() != w.Days() {
		t.Fatalf("previous has %d days, want %d", prev.Days(), w.Days())
	}
}

func TestCompareReadings(t *testing.T) {
	current := []MetricReading{
		{Key: MetricFinished, Direction: DirectionHigherIsBetter, Value: ptr(120)},
		{Key: MetricAvgFRTMins, Direction: DirectionLowerIsBetter, Value: ptr(9)},
		{Key: MetricReopenRate, Direction: DirectionLowerIsBetter, Value: ptr(4)},
		{Key: MetricAvgWaitMins, Direction: DirectionLowerIsBetter},
	}
	previous := []MetricReading{
		{Key: MetricFinished, Value: ptr(100)},
		{Key: MetricAvgFRTMins, Value: ptr(12)},
		{Key: MetricReopenRate, Value: ptr(0)},
		{Key: MetricAvgWaitMins, Value: ptr(3)},
	}
	got := CompareReadings(current, previous)
	if got[0].DeltaPct == nil || *got[0].DeltaPct != 20 || got[0].Change != ChangeImproved {
		t.Fatalf("finished = %+v", got[0])
	}
	// A lower FRT is the good direction; the sign of the delta alone would call it a decline.
	if got[1].DeltaPct == nil || *got[1].DeltaPct != -25 || got[1].Change != ChangeImproved {
		t.Fatalf("frt = %+v", got[1])
	}
	// From zero there is no percentage, but the absolute move still tells the story.
	if got[2].DeltaPct != nil || got[2].Delta == nil || *got[2].Delta != 4 || got[2].Change != ChangeWorsened {
		t.Fatalf("reopen = %+v", got[2])
	}
	if got[3].Change != ChangeUnknown || got[3].Delta != nil {
		t.Fatalf("wait = %+v, want unknown when this period has no value", got[3])
	}
}

func TestDigestTrendOffersOnlyTheSeriesTheTrendBuilds(t *testing.T) {
	queue := TrendSeries{MetricKey: MetricPendingStock, Kind: MetricKindCount, Direction: DirectionLowerIsBetter, Available: true,
		Points: []TrendPoint{{Bucket: "2026-08", Value: 4}}}
	got, err := DigestTrend(Trend{Available: true, Series: []TrendSeries{queue}}, []string{MetricPendingStock})
	// The page draws "Fila acumulada" from this series; the assistant must be able to ask for it by name.
	if err != nil || got[0].Direction != DirectionLowerIsBetter || len(got[0].Points) != 1 {
		t.Fatalf("got = %+v err %v", got, err)
	}
	// First response time is a period metric, never a monthly series: asking for it must say so, not return a blank.
	if _, err := DigestTrend(Trend{}, []string{MetricAvgFRTMins}); !errors.Is(err, ErrUnknownMetric) {
		t.Fatalf("err = %v, want ErrUnknownMetric for a metric the trend never builds", err)
	}
	for _, key := range TrendMetricKeys() {
		if _, err := DigestTrend(Trend{}, []string{key}); err != nil {
			t.Fatalf("trend key %s refused: %v", key, err)
		}
	}
}
