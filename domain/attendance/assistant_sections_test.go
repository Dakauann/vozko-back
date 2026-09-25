package attendance

import (
	"errors"
	"fmt"
	"testing"
)

func richSummary() *SummarySection {
	rating, frtSLA := 4.6, 91.0
	s := &SummarySection{
		KPIs:               OverviewKPIs{AvgRating: &rating, CSATAvailable: true, FRTSLAPercent: &frtSLA, SLAAvailable: true, UnassignedBacklog: 7},
		StatusDistribution: StatusDistribution{Finished: 80, Ongoing: 15, Pending: 5, Total: 100},
		FRT:                OverviewFRT{MedianMins: ptr(3), HumanAvgMins: ptr(6), AIAvgMins: ptr(0.2), Available: true},
		Hourly:             []HourlyPoint{{Hour: 9, Count: 12}, {Hour: 10, Count: 30}},
		ChannelMix:         []ChannelSlice{{Channel: "whatsapp", Count: 90, Pct: 90}},
		Projections: []MetricProjection{
			{MetricKey: MetricFinished, Target: ptr(100), Verdict: VerdictAtRisk},
			{MetricKey: MetricAvgFRTMins, Verdict: VerdictNoTarget},
		},
		Standing: Standing{TargetsSet: 1, AtRisk: 1, Available: true},
	}
	for i := 0; i < 30; i++ {
		s.Quality.Rows = append(s.Quality.Rows, QualityRow{ActorID: fmt.Sprint(i), Closes: int64(i)})
		s.Revenue.ByOwner = append(s.Revenue.ByOwner, RevenueOwnerRow{OwnerID: fmt.Sprint(i), ValueCents: int64(i * 100)})
	}
	return s
}

func TestSummaryBlocksCoverWhatThePageShows(t *testing.T) {
	s := richSummary()
	for _, block := range SummaryBlocks() {
		t.Run(string(block), func(t *testing.T) {
			if _, err := DigestSummaryBlock(s, block); err != nil {
				t.Fatalf("block %s: %v", block, err)
			}
		})
	}
	if _, err := DigestSummaryBlock(s, "nps"); !errors.Is(err, ErrUnknownBlock) {
		t.Fatalf("err = %v, want ErrUnknownBlock", err)
	}
}

func TestServiceLevelsCarrySLAAndCSAT(t *testing.T) {
	got, _ := DigestSummaryBlock(richSummary(), BlockServiceLevels)
	sl := got.(ServiceLevels)
	if sl.CSATAvg == nil || *sl.CSATAvg != 4.6 || sl.FRTSLAPct == nil || *sl.FRTSLAPct != 91 || sl.UnassignedBacklog != 7 || sl.Status.Total != 100 {
		t.Fatalf("service levels = %+v", sl)
	}
}

func TestGoalsKeepOnlyMetricsWithATarget(t *testing.T) {
	got, _ := DigestSummaryBlock(richSummary(), BlockGoals)
	goals := got.(GoalsDigest)
	if len(goals.Targets) != 1 || goals.Targets[0].MetricKey != MetricFinished || goals.Standing.AtRisk != 1 {
		t.Fatalf("goals = %+v", goals)
	}
}

func TestPerPersonListsAreCappedAndRanked(t *testing.T) {
	q, _ := DigestSummaryBlock(richSummary(), BlockQuality)
	quality := q.(QualityDigest)
	// The busiest closers first: they are the ones whose durability moves the team number.
	if len(quality.Rows) != MaxAssistantListItems || quality.TotalRows != 30 || quality.Rows[0].Closes != 29 {
		t.Fatalf("quality = %d rows of %d, first %+v", len(quality.Rows), quality.TotalRows, quality.Rows[0])
	}
	r, _ := DigestSummaryBlock(richSummary(), BlockRevenue)
	revenue := r.(RevenueDigest)
	if len(revenue.ByOwner) != MaxAssistantListItems || revenue.TotalOwners != 30 || revenue.ByOwner[0].ValueCents != 2900 {
		t.Fatalf("revenue = %d owners of %d, first %+v", len(revenue.ByOwner), revenue.TotalOwners, revenue.ByOwner[0])
	}
}

func TestDigestTeamCarriesRevenueProductivityAndMessages(t *testing.T) {
	cents, ticket, perHour := int64(150000), 7500.0, 3.2
	r := TeamRanking{Available: true, Members: []RankedMember{{
		MemberRow:      MemberRow{DisplayName: "Ana", Presence: "online", TotalMessages: 400, AvgMessages: ptr(12.5)},
		RevenueCents:   &cents,
		Currency:       "BRL",
		WonCount:       20,
		AvgTicketCents: &ticket,
		PerOnlineHour:  &perHour,
	}}}
	m := DigestTeam(r, 1, false).Members[0]
	if m.RevenueCents == nil || *m.RevenueCents != cents || m.WonCount != 20 || m.Currency != "BRL" || m.PerOnlineHour == nil || m.Presence != "online" || m.TotalMessages != 400 {
		t.Fatalf("member = %+v", m)
	}
}

func TestDigestDepartmentsCapsTheList(t *testing.T) {
	rows := make([]DepartmentRow, 0, 14)
	for i := 0; i < 14; i++ {
		rows = append(rows, DepartmentRow{DepartmentName: fmt.Sprint(i), Finished: int64(i)})
	}
	got := DigestDepartments(rows)
	if len(got.Rows) != MaxAssistantListItems || got.Total != 14 || got.Rows[0].Finished != 13 {
		t.Fatalf("departments = %+v", got)
	}
}

func TestDigestStagesSummarisesFunnels(t *testing.T) {
	st := OverviewStages{Available: true, Stuck: 3, Funnels: []StageFunnelGroup{{
		FunnelName: "Vendas", Total: 40, Stuck: 3,
		Stages: []StageRow{{StageName: "Novo", Total: 30, Stuck: 3}, {StageName: "Ganho", Total: 10, IsWon: true}},
	}}}
	got := DigestStages(st)
	if got.Stuck != 3 || len(got.Funnels) != 1 || got.Funnels[0].Stages != 2 || got.Funnels[0].Name != "Vendas" {
		t.Fatalf("stages = %+v", got)
	}
}

func TestDigestReworkRanksByReopens(t *testing.T) {
	rw := OverviewRework{Available: true, Team: ReworkRow{Finished: 100, Reopened: 9}}
	for i := 0; i < 20; i++ {
		rw.Rows = append(rw.Rows, ReworkRow{DisplayName: fmt.Sprint(i), Reopened: int64(i)})
	}
	got := DigestRework(rw)
	if len(got.Rows) != MaxAssistantListItems || got.TotalRows != 20 || got.Rows[0].Reopened != 19 || got.Team.Reopened != 9 {
		t.Fatalf("rework = %+v", got)
	}
}

func TestDigestLiveDropsThePerAgentList(t *testing.T) {
	live := LiveSection{Live: OverviewLive{Online: 4, InCall: 1, Free: 3, HasData: true, Agents: []OverviewLiveAgent{{UserID: "u"}}}}
	got := DigestLive(live)
	if got.Live.Online != 4 || got.Live.Agents != nil {
		t.Fatalf("live = %+v, want counts without the per-agent ids", got.Live)
	}
}
