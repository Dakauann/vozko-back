package attendance

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type SummaryBlock string

const (
	BlockTiming        SummaryBlock = "timing"
	BlockServiceLevels SummaryBlock = "service_levels"
	BlockAI            SummaryBlock = "ai"
	BlockMessaging     SummaryBlock = "messaging"
	BlockClosing       SummaryBlock = "closing"
	BlockQuality       SummaryBlock = "quality"
	BlockRevenue       SummaryBlock = "revenue"
	BlockGoals         SummaryBlock = "goals"
	BlockHourly        SummaryBlock = "hourly"
	BlockChannels      SummaryBlock = "channels"
	BlockDefinitions   SummaryBlock = "definitions"
)

var ErrUnknownBlock = errors.New("attendance: unknown summary block")

func SummaryBlocks() []SummaryBlock {
	return []SummaryBlock{BlockTiming, BlockServiceLevels, BlockAI, BlockMessaging, BlockClosing, BlockQuality, BlockRevenue, BlockGoals, BlockHourly, BlockChannels, BlockDefinitions}
}

type TimingDigest struct {
	FRT           OverviewFRT `json:"first_response"`
	AvgWaitMins   *float64    `json:"avg_wait_mins"`
	AvgHandleMins *float64    `json:"avg_handle_mins"`
}

type ServiceLevels struct {
	FRTSLAPct         *float64           `json:"frt_sla_pct"`
	ResolutionSLAPct  *float64           `json:"resolution_sla_pct"`
	SLAAvailable      bool               `json:"sla_available"`
	CSATAvg           *float64           `json:"csat_avg"`
	CSATAvailable     bool               `json:"csat_available"`
	Status            StatusDistribution `json:"status"`
	UnassignedBacklog int64              `json:"unassigned_backlog"`
}

type ClosingDigest struct {
	FinishedBySource OverviewFinishedBySource `json:"finished_by_source"`
	Reopen           OverviewReopen           `json:"reopen"`
}

type QualityDigest struct {
	Threshold   float64      `json:"threshold"`
	Team        QualityRow   `json:"team"`
	Rows        []QualityRow `json:"rows"`
	TotalRows   int          `json:"total_rows"`
	NotCaptured int64        `json:"not_captured"`
	Available   bool         `json:"available"`
	Reason      string       `json:"reason,omitempty"`
}

type RevenueDigest struct {
	Currencies      []RevenueByCurrency `json:"currencies"`
	BySource        []RevenueSourceRow  `json:"by_source"`
	ByOwner         []RevenueOwnerRow   `json:"by_owner"`
	TotalOwners     int                 `json:"total_owners"`
	WonWithoutValue int64               `json:"won_without_value"`
	Unattributed    int64               `json:"unattributed"`
	MixedCurrencies bool                `json:"mixed_currencies"`
	Available       bool                `json:"available"`
	Reason          string              `json:"reason,omitempty"`
}

type GoalsDigest struct {
	Period   Period             `json:"period"`
	Standing Standing           `json:"standing"`
	Targets  []MetricProjection `json:"targets"`
}

func DigestSummaryBlock(s *SummarySection, block SummaryBlock) (any, error) {
	if s == nil {
		s = &SummarySection{}
	}
	switch block {
	case BlockTiming:
		return TimingDigest{FRT: s.FRT, AvgWaitMins: s.KPIs.AvgWaitMins, AvgHandleMins: s.KPIs.AvgHandleMins}, nil
	case BlockServiceLevels:
		return ServiceLevels{
			FRTSLAPct:         s.KPIs.FRTSLAPercent,
			ResolutionSLAPct:  s.KPIs.ResolutionSLAPercent,
			SLAAvailable:      s.KPIs.SLAAvailable,
			CSATAvg:           s.KPIs.AvgRating,
			CSATAvailable:     s.KPIs.CSATAvailable,
			Status:            s.StatusDistribution,
			UnassignedBacklog: s.KPIs.UnassignedBacklog,
		}, nil
	case BlockAI:
		return s.AI, nil
	case BlockMessaging:
		return s.Messaging, nil
	case BlockClosing:
		return ClosingDigest{FinishedBySource: s.FinishedBySource, Reopen: s.Reopen}, nil
	case BlockQuality:
		return digestQuality(s.Quality), nil
	case BlockRevenue:
		return digestRevenue(s.Revenue), nil
	case BlockGoals:
		return digestGoals(s), nil
	case BlockHourly:
		return nonNil(s.Hourly), nil
	case BlockChannels:
		return nonNil(s.ChannelMix), nil
	case BlockDefinitions:
		return s.Definitions, nil
	}
	return nil, fmt.Errorf("%w: %q, valid blocks are %s", ErrUnknownBlock, block, blockList())
}

func blockList() string {
	names := make([]string, 0, len(SummaryBlocks()))
	for _, b := range SummaryBlocks() {
		names = append(names, string(b))
	}
	return strings.Join(names, ", ")
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func topN[T any](items []T, weight func(T) float64) ([]T, int) {
	ranked := append([]T(nil), items...)
	sort.SliceStable(ranked, func(i, j int) bool { return weight(ranked[i]) > weight(ranked[j]) })
	if len(ranked) > MaxAssistantListItems {
		ranked = ranked[:MaxAssistantListItems]
	}
	return nonNil(ranked), len(items)
}

func digestQuality(q Quality) QualityDigest {
	rows, total := topN(q.Rows, func(r QualityRow) float64 { return float64(r.Closes) })
	return QualityDigest{
		Threshold:   q.Threshold,
		Team:        q.Team,
		Rows:        rows,
		TotalRows:   total,
		NotCaptured: q.NotCaptured,
		Available:   q.Available,
		Reason:      q.Reason,
	}
}

func digestRevenue(r Revenue) RevenueDigest {
	owners, total := topN(r.ByOwner, func(o RevenueOwnerRow) float64 { return float64(o.ValueCents) })
	return RevenueDigest{
		Currencies:      nonNil(r.Currencies),
		BySource:        nonNil(r.BySource),
		ByOwner:         owners,
		TotalOwners:     total,
		WonWithoutValue: r.WonWithoutValue,
		Unattributed:    r.Unattributed,
		MixedCurrencies: r.MixedCurrencies,
		Available:       r.Available,
		Reason:          r.Reason,
	}
}

func digestGoals(s *SummarySection) GoalsDigest {
	out := GoalsDigest{Period: s.Period, Standing: s.Standing, Targets: []MetricProjection{}}
	for _, p := range s.Projections {
		if p.Target != nil {
			out.Targets = append(out.Targets, p)
		}
	}
	return out
}

type DepartmentsDigest struct {
	Rows  []DepartmentRow `json:"rows"`
	Total int             `json:"total"`
}

func DigestDepartments(rows []DepartmentRow) DepartmentsDigest {
	top, total := topN(rows, func(r DepartmentRow) float64 { return float64(r.Finished + r.Ongoing + r.Pending) })
	return DepartmentsDigest{Rows: top, Total: total}
}

type FunnelDigest struct {
	Name        string  `json:"name"`
	IsDefault   bool    `json:"is_default"`
	Total       int64   `json:"total"`
	Stuck       int64   `json:"stuck"`
	PctOfStaged float64 `json:"pct_of_staged"`
	Stages      int     `json:"stages"`
}

type StagesDigest struct {
	Funnels         []FunnelDigest `json:"funnels"`
	StagedEngaged   int64          `json:"staged_engaged"`
	UnstagedEngaged int64          `json:"unstaged_engaged"`
	Stuck           int64          `json:"stuck"`
	Available       bool           `json:"available"`
}

func DigestStages(st OverviewStages) StagesDigest {
	out := StagesDigest{
		Funnels:         []FunnelDigest{},
		StagedEngaged:   st.StagedEngaged,
		UnstagedEngaged: st.UnstagedEngaged,
		Stuck:           st.Stuck,
		Available:       st.Available,
	}
	for _, f := range st.Funnels {
		out.Funnels = append(out.Funnels, FunnelDigest{
			Name:        f.FunnelName,
			IsDefault:   f.IsDefault,
			Total:       f.Total,
			Stuck:       f.Stuck,
			PctOfStaged: round2(f.PctOfStaged),
			Stages:      len(f.Stages),
		})
	}
	return out
}

type ReworkDigest struct {
	Team          ReworkRow   `json:"team"`
	Unassigned    ReworkRow   `json:"unassigned"`
	Rows          []ReworkRow `json:"rows"`
	TotalRows     int         `json:"total_rows"`
	Currency      string      `json:"currency,omitempty"`
	CostAvailable bool        `json:"cost_available"`
	Available     bool        `json:"available"`
	Reason        string      `json:"reason,omitempty"`
}

func DigestRework(r OverviewRework) ReworkDigest {
	rows, total := topN(r.Rows, func(row ReworkRow) float64 { return float64(row.Reopened) })
	return ReworkDigest{
		Team:          r.Team,
		Unassigned:    r.Unassigned,
		Rows:          rows,
		TotalRows:     total,
		Currency:      r.Currency,
		CostAvailable: r.CostAvailable,
		Available:     r.Available,
		Reason:        r.Reason,
	}
}

func DigestLive(l LiveSection) LiveSection {
	l.Live.Agents = nil
	return l
}
