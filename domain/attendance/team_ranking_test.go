package attendance

import "testing"

func humanRow(id, name string, resolved, open, pending int64) MemberRow {
	return MemberRow{
		ActorID:     id,
		ActorKind:   ActorKindHuman,
		DisplayName: name,
		Resolved:    resolved,
		Open:        open,
		Pending:     pending,
	}
}

func TestBuildTeamRankingExcludesAdjacentsFromTheAverage(t *testing.T) {
	rows := []MemberRow{
		humanRow("u1", "Bella", 364, 10, 5),
		humanRow("u2", "Ana", 276, 8, 4),
		{ActorID: "sys", ActorKind: ActorKindSystem, DisplayName: "Sistema", Resolved: 1280},
	}
	got := BuildTeamRanking(rows, RankByResolved, maturePeriod(), nil, nil, nil)

	if !got.Available {
		t.Fatalf("BuildTeamRanking() Available = false, want true")
	}
	if len(got.Members) != 2 || len(got.Adjacent) != 1 {
		t.Fatalf("BuildTeamRanking() split = %d human / %d adjacent, want 2/1", len(got.Members), len(got.Adjacent))
	}
	if got.TeamAverage == nil || *got.TeamAverage != 320 {
		t.Fatalf("BuildTeamRanking() TeamAverage = %v, want 320 over humans only", got.TeamAverage)
	}
	for _, row := range got.Adjacent {
		if row.Class != "" {
			t.Fatalf("BuildTeamRanking() adjacent row carries class %q, want none", row.Class)
		}
	}
}

func TestBuildTeamRankingClassNeedsASample(t *testing.T) {
	rows := []MemberRow{
		humanRow("u1", "Bella", 364, 0, 0),
		humanRow("u2", "Miqueias", 19, 0, 0),
	}
	got := BuildTeamRanking(rows, RankByResolved, maturePeriod(), nil, nil, nil)

	byName := map[string]RankedMember{}
	for _, row := range got.Members {
		byName[row.DisplayName] = row
	}
	if byName["Miqueias"].Class != ClassInsufficientData {
		t.Fatalf("BuildTeamRanking() class below the sample floor = %q, want %q", byName["Miqueias"].Class, ClassInsufficientData)
	}
	if byName["Bella"].Class != ClassElite {
		t.Fatalf("BuildTeamRanking() Bella class = %q, want %q", byName["Bella"].Class, ClassElite)
	}
	if byName["Miqueias"].PctOfTeamAvg == nil {
		t.Fatalf("BuildTeamRanking() withheld PctOfTeamAvg from a thin row; the number is still measured")
	}
}

func TestBuildTeamRankingPerHourNeedsPresence(t *testing.T) {
	rows := []MemberRow{
		humanRow("u1", "Bella", 364, 0, 0),
		humanRow("u2", "Ana", 276, 0, 0),
	}
	presence := map[string]int64{"u1": 36_000_000}

	got := BuildTeamRanking(rows, RankByResolved, maturePeriod(), presence, nil, nil)
	byName := map[string]RankedMember{}
	for _, row := range got.Members {
		byName[row.DisplayName] = row
	}

	if byName["Bella"].PerOnlineHour == nil {
		t.Fatalf("BuildTeamRanking() PerOnlineHour = nil for a member with presence")
	}
	if byName["Ana"].PerOnlineHour != nil {
		t.Fatalf("BuildTeamRanking() PerOnlineHour = %v for a member with no presence, want nil", *byName["Ana"].PerOnlineHour)
	}
}

func TestBuildTeamRankingPerOpenDayNeedsAPeriod(t *testing.T) {
	rows := []MemberRow{humanRow("u1", "Bella", 364, 0, 0)}

	withPeriod := BuildTeamRanking(rows, RankByResolved, maturePeriod(), nil, nil, nil)
	if withPeriod.Members[0].PerOpenDay == nil {
		t.Fatalf("BuildTeamRanking() PerOpenDay = nil with an available period")
	}

	withoutPeriod := BuildTeamRanking(rows, RankByResolved, Period{Reason: ReasonNoSchedule}, nil, nil, nil)
	if withoutPeriod.Members[0].PerOpenDay != nil {
		t.Fatalf("BuildTeamRanking() PerOpenDay = %v without a period, want nil", *withoutPeriod.Members[0].PerOpenDay)
	}
}

func TestBuildTeamRankingByRevenue(t *testing.T) {
	rows := []MemberRow{
		humanRow("u1", "Bella", 364, 0, 0),
		humanRow("u2", "Ana", 276, 0, 0),
	}
	revenue := map[string]OwnerRevenue{
		"u1": {Currency: "BRL", ValueCents: 23_015_31, WonCount: 725},
		"u2": {Currency: "BRL", ValueCents: 10_938_29, WonCount: 493},
	}

	got := BuildTeamRanking(rows, RankByRevenue, maturePeriod(), nil, revenue, nil)
	if got.RankMetricKey != RankByRevenue {
		t.Fatalf("BuildTeamRanking() RankMetricKey = %q, want %q", got.RankMetricKey, RankByRevenue)
	}
	if got.Members[0].DisplayName != "Bella" {
		t.Fatalf("BuildTeamRanking() top by revenue = %q, want Bella", got.Members[0].DisplayName)
	}
	if got.Members[0].AvgTicketCents == nil {
		t.Fatalf("BuildTeamRanking() AvgTicketCents = nil, want an average ticket")
	}
	if got.Totals.Currency != "BRL" || got.Totals.RevenueCents == nil {
		t.Fatalf("BuildTeamRanking() totals = %+v, want a single-currency total", got.Totals)
	}
}

func TestBuildTeamRankingTotalsRefuseMixedCurrencies(t *testing.T) {
	rows := []MemberRow{
		humanRow("u1", "Bella", 30, 0, 0),
		humanRow("u2", "Ana", 30, 0, 0),
	}
	revenue := map[string]OwnerRevenue{
		"u1": {Currency: "BRL", ValueCents: 1000, WonCount: 1},
		"u2": {Currency: "USD", ValueCents: 2000, WonCount: 1},
	}

	got := BuildTeamRanking(rows, RankByRevenue, maturePeriod(), nil, revenue, nil)
	if got.Totals.RevenueCents != nil {
		t.Fatalf("BuildTeamRanking() totals summed across currencies: %v", *got.Totals.RevenueCents)
	}
}

func TestBuildTeamRankingWithNoHumanRows(t *testing.T) {
	rows := []MemberRow{{ActorID: "ai", ActorKind: ActorKindAI, DisplayName: "IA", Resolved: 100}}
	got := BuildTeamRanking(rows, RankByResolved, maturePeriod(), nil, nil, nil)

	if got.Available {
		t.Fatalf("BuildTeamRanking() with no human rows Available = true, want false")
	}
	if got.Reason != ReasonNoHumanTeamRows {
		t.Fatalf("BuildTeamRanking() Reason = %q, want %q", got.Reason, ReasonNoHumanTeamRows)
	}
	if len(got.Adjacent) != 1 {
		t.Fatalf("BuildTeamRanking() dropped the adjacent rows")
	}
}

func TestBuildTeamRankingEmpty(t *testing.T) {
	got := BuildTeamRanking(nil, "", maturePeriod(), nil, nil, nil)
	if got.Available || got.Reason != ReasonNoTeamRows {
		t.Fatalf("BuildTeamRanking() with no rows = %+v, want unavailable and %q", got, ReasonNoTeamRows)
	}
	if got.RankMetricKey != DefaultRankMetric {
		t.Fatalf("BuildTeamRanking() RankMetricKey = %q, want the declared default %q", got.RankMetricKey, DefaultRankMetric)
	}
}

func TestNormalizeRankMetric(t *testing.T) {
	cases := map[string]string{
		RankByResolved: RankByResolved,
		RankByVolume:   RankByVolume,
		RankByRevenue:  RankByRevenue,
		"nonsense":     DefaultRankMetric,
		"":             DefaultRankMetric,
	}
	for in, want := range cases {
		if got := NormalizeRankMetric(in); got != want {
			t.Fatalf("NormalizeRankMetric(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildStanding(t *testing.T) {
	projections := []MetricProjection{
		{MetricKey: "a", Target: ptr(10), Verdict: VerdictOnTrack},
		{MetricKey: "b", Target: ptr(10), Verdict: VerdictOffTrack},
		{MetricKey: "c", Target: ptr(10), Verdict: VerdictOffTrack},
		{MetricKey: "d", Verdict: VerdictNoTarget},
	}
	got := BuildStanding(projections, DefaultClusterBands())

	if got.TargetsSet != 3 {
		t.Fatalf("BuildStanding() TargetsSet = %d, want 3", got.TargetsSet)
	}
	if got.OnTrack != 1 || got.OffTrack != 2 {
		t.Fatalf("BuildStanding() = %+v, want 1 on track and 2 off", got)
	}
	if got.Cluster != ClusterImprovement {
		t.Fatalf("BuildStanding() Cluster = %q, want %q", got.Cluster, ClusterImprovement)
	}
}

func TestBuildStandingWithoutTargetsIsNeverCritical(t *testing.T) {
	got := BuildStanding([]MetricProjection{
		{MetricKey: "a", Verdict: VerdictNoTarget},
		{MetricKey: "b", Verdict: VerdictNoTarget},
	}, DefaultClusterBands())

	if got.Available {
		t.Fatalf("BuildStanding() with no targets Available = true, want false")
	}
	if got.Cluster != "" {
		t.Fatalf("BuildStanding() Cluster = %q, want empty with no targets set", got.Cluster)
	}
	if got.Reason != ReasonNoTargetsSet {
		t.Fatalf("BuildStanding() Reason = %q, want %q", got.Reason, ReasonNoTargetsSet)
	}
}

func TestBuildStandingClusterBands(t *testing.T) {
	cases := []struct {
		onTrack int
		total   int
		want    string
	}{
		{4, 4, ClusterExcellence},
		{3, 4, ClusterOnTrack},
		{2, 4, ClusterImprovement},
		{0, 4, ClusterCritical},
	}
	for _, tc := range cases {
		projections := make([]MetricProjection, 0, tc.total)
		for i := 0; i < tc.total; i++ {
			verdict := VerdictOffTrack
			if i < tc.onTrack {
				verdict = VerdictOnTrack
			}
			projections = append(projections, MetricProjection{Target: ptr(1), Verdict: verdict})
		}
		got := BuildStanding(projections, DefaultClusterBands())
		if got.Cluster != tc.want {
			t.Fatalf("BuildStanding(%d/%d) Cluster = %q, want %q", tc.onTrack, tc.total, got.Cluster, tc.want)
		}
	}
}
