package attendance

import "sort"

const (
	ActorKindHuman  = "human"
	ActorKindAI     = "ai"
	ActorKindSystem = "system"
)

const (
	RankByResolved = "resolved"
	RankByVolume   = "volume"
	RankByRevenue  = "revenue_cents"
)

const DefaultRankMetric = RankByResolved

const MinClosesForClass = 20

const (
	ReasonNoTeamRows       = "no_team_rows"
	ReasonNoHumanTeamRows  = "no_human_team_rows"
	ReasonRankingUnwatched = "ranking_inputs_unavailable"
)

type MemberClass string

const (
	ClassElite            MemberClass = "elite"
	ClassSolid            MemberClass = "solid"
	ClassBelow            MemberClass = "below"
	ClassCritical         MemberClass = "critical"
	ClassInsufficientData MemberClass = "insufficient_data"
)

type MemberClassBand struct {
	MinPctOfTeamAvg float64     `json:"min_pct_of_team_avg"`
	Class           MemberClass `json:"class"`
}

type MemberClassBands []MemberClassBand

func DefaultMemberClassBands() MemberClassBands {
	return MemberClassBands{
		{MinPctOfTeamAvg: 150, Class: ClassElite},
		{MinPctOfTeamAvg: 100, Class: ClassSolid},
		{MinPctOfTeamAvg: 60, Class: ClassBelow},
		{MinPctOfTeamAvg: 0, Class: ClassCritical},
	}
}

func (b MemberClassBands) classFor(pct float64) MemberClass {
	bands := make(MemberClassBands, len(b))
	copy(bands, b)
	sort.Slice(bands, func(i, j int) bool { return bands[i].MinPctOfTeamAvg > bands[j].MinPctOfTeamAvg })
	for _, band := range bands {
		if pct >= band.MinPctOfTeamAvg {
			return band.Class
		}
	}
	return ClassCritical
}

func NormalizeRankMetric(key string) string {
	switch key {
	case RankByResolved, RankByVolume, RankByRevenue:
		return key
	}
	return DefaultRankMetric
}

type OwnerRevenue struct {
	Currency   string
	ValueCents int64
	WonCount   int64
}

type RankedMember struct {
	MemberRow
	RankMetricValue float64     `json:"rank_metric_value"`
	PerOpenDay      *float64    `json:"per_open_day"`
	PerOnlineHour   *float64    `json:"per_online_hour"`
	OnlineMS        int64       `json:"online_ms"`
	PctOfTeamAvg    *float64    `json:"pct_of_team_avg"`
	RevenueCents    *int64      `json:"revenue_cents"`
	Currency        string      `json:"currency,omitempty"`
	AvgTicketCents  *float64    `json:"avg_ticket_cents"`
	WonCount        int64       `json:"won_count"`
	Class           MemberClass `json:"class,omitempty"`
}

type TeamTotals struct {
	Members         int      `json:"members"`
	Open            int64    `json:"open"`
	Pending         int64    `json:"pending"`
	Resolved        int64    `json:"resolved"`
	RankMetricValue float64  `json:"rank_metric_value"`
	PerOpenDay      *float64 `json:"per_open_day"`
	PerOnlineHour   *float64 `json:"per_online_hour"`
	OnlineMS        int64    `json:"online_ms"`
	RevenueCents    *int64   `json:"revenue_cents"`
	Currency        string   `json:"currency,omitempty"`
	AvgTicketCents  *float64 `json:"avg_ticket_cents"`
	WonCount        int64    `json:"won_count"`
}

type TeamRanking struct {
	RankMetricKey string         `json:"rank_metric_key"`
	MinSample     int            `json:"min_sample"`
	TeamAverage   *float64       `json:"team_average"`
	Members       []RankedMember `json:"members"`
	Adjacent      []RankedMember `json:"adjacent"`
	Totals        TeamTotals     `json:"totals"`
	AdjacentTotal TeamTotals     `json:"adjacent_totals"`
	Available     bool           `json:"available"`
	Reason        string         `json:"reason,omitempty"`
}

func UnavailableTeamRanking(reason string) TeamRanking {
	return TeamRanking{
		RankMetricKey: DefaultRankMetric,
		MinSample:     MinClosesForClass,
		Members:       []RankedMember{},
		Adjacent:      []RankedMember{},
		Reason:        reason,
	}
}

func BuildTeamRanking(
	rows []MemberRow,
	rankMetric string,
	p Period,
	onlineMS map[string]int64,
	revenue map[string]OwnerRevenue,
	bands MemberClassBands,
) TeamRanking {
	metric := NormalizeRankMetric(rankMetric)
	out := TeamRanking{
		RankMetricKey: metric,
		MinSample:     MinClosesForClass,
		Members:       []RankedMember{},
		Adjacent:      []RankedMember{},
	}
	if len(rows) == 0 {
		out.Reason = ReasonNoTeamRows
		return out
	}
	if len(bands) == 0 {
		bands = DefaultMemberClassBands()
	}

	for _, row := range rows {
		ranked := rankMember(row, metric, p, onlineMS, revenue)
		if row.ActorKind == ActorKindHuman {
			out.Members = append(out.Members, ranked)
			continue
		}
		out.Adjacent = append(out.Adjacent, ranked)
	}

	out.Totals = totalsOf(out.Members, p)
	out.AdjacentTotal = totalsOf(out.Adjacent, p)

	if len(out.Members) == 0 {
		sortRanked(out.Adjacent)
		out.Reason = ReasonNoHumanTeamRows
		return out
	}

	average := out.Totals.RankMetricValue / float64(len(out.Members))
	if isFinite(average) && average > 0 {
		out.TeamAverage = round2Ptr(average)
	}

	for i := range out.Members {
		member := &out.Members[i]
		if out.TeamAverage == nil {
			member.Class = ClassInsufficientData
			continue
		}
		pct, ok := ratioPct(member.RankMetricValue, *out.TeamAverage)
		if !ok {
			member.Class = ClassInsufficientData
			continue
		}
		member.PctOfTeamAvg = &pct
		if member.Resolved < MinClosesForClass {
			member.Class = ClassInsufficientData
			continue
		}
		member.Class = bands.classFor(pct)
	}

	sortRanked(out.Members)
	sortRanked(out.Adjacent)
	out.Available = true
	return out
}

func rankMember(
	row MemberRow,
	metric string,
	p Period,
	onlineMS map[string]int64,
	revenue map[string]OwnerRevenue,
) RankedMember {
	out := RankedMember{MemberRow: row}

	if own, found := revenue[row.ActorID]; found && row.ActorID != "" {
		value := own.ValueCents
		out.RevenueCents = &value
		out.Currency = own.Currency
		out.WonCount = own.WonCount
		out.AvgTicketCents = avgTicket(own.ValueCents, own.WonCount)
	}

	switch metric {
	case RankByVolume:
		out.RankMetricValue = float64(row.Open + row.Pending + row.Resolved)
	case RankByRevenue:
		if out.RevenueCents != nil {
			out.RankMetricValue = float64(*out.RevenueCents)
		}
	default:
		out.RankMetricValue = float64(row.Resolved)
	}

	if p.Available && p.OpenDaysDone > 0 {
		perDay := out.RankMetricValue / float64(p.OpenDaysDone)
		if isFinite(perDay) {
			out.PerOpenDay = round2Ptr(perDay)
		}
	}

	if ms, found := onlineMS[row.ActorID]; found && ms > 0 && row.ActorID != "" {
		out.OnlineMS = ms
		perHour := out.RankMetricValue / (float64(ms) / 3600000)
		if isFinite(perHour) {
			out.PerOnlineHour = round2Ptr(perHour)
		}
	}
	return out
}

func totalsOf(rows []RankedMember, p Period) TeamTotals {
	out := TeamTotals{Members: len(rows)}
	currencies := map[string]struct{}{}
	var revenueCents int64
	hasRevenue := false

	for _, row := range rows {
		out.Open += row.Open
		out.Pending += row.Pending
		out.Resolved += row.Resolved
		out.RankMetricValue += row.RankMetricValue
		out.OnlineMS += row.OnlineMS
		out.WonCount += row.WonCount
		if row.RevenueCents != nil {
			hasRevenue = true
			revenueCents += *row.RevenueCents
			currencies[row.Currency] = struct{}{}
		}
	}
	out.RankMetricValue = round2(out.RankMetricValue)

	if p.Available && p.OpenDaysDone > 0 {
		perDay := out.RankMetricValue / float64(p.OpenDaysDone)
		if isFinite(perDay) {
			out.PerOpenDay = round2Ptr(perDay)
		}
	}
	if out.OnlineMS > 0 {
		perHour := out.RankMetricValue / (float64(out.OnlineMS) / 3600000)
		if isFinite(perHour) {
			out.PerOnlineHour = round2Ptr(perHour)
		}
	}
	if hasRevenue && len(currencies) == 1 {
		for currency := range currencies {
			out.Currency = currency
		}
		total := revenueCents
		out.RevenueCents = &total
		out.AvgTicketCents = avgTicket(revenueCents, out.WonCount)
	}
	return out
}

func sortRanked(rows []RankedMember) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].RankMetricValue != rows[j].RankMetricValue {
			return rows[i].RankMetricValue > rows[j].RankMetricValue
		}
		if rows[i].Resolved != rows[j].Resolved {
			return rows[i].Resolved > rows[j].Resolved
		}
		return rows[i].DisplayName < rows[j].DisplayName
	})
}
