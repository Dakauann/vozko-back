package attendance

import (
	"math"
	"sort"
)

const DefaultStageStuckDays = 7

func EffectiveStuckDays(rotDays *int) int {
	if rotDays != nil && *rotDays > 0 {
		return *rotDays
	}
	return DefaultStageStuckDays
}

type StageTally struct {
	FunnelID        string
	FunnelName      string
	FunnelIsDefault bool

	StageID   string
	StageName string
	Color     string
	Position  int
	IsWon     bool
	IsLost    bool
	RotDays   *int

	Engaged  int64
	Shell    int64
	Finished int64
	Ongoing  int64
	Pending  int64

	AvgOpenDays    *float64
	OldestOpenDays *float64
	Stuck          int64
}

type StageRow struct {
	StageID   string `json:"stage_id"`
	StageName string `json:"stage_name"`
	Color     string `json:"color,omitempty"`
	Position  int    `json:"position"`
	IsWon     bool   `json:"is_won"`
	IsLost    bool   `json:"is_lost"`

	Engaged int64 `json:"engaged"`
	Shell   int64 `json:"shell"`
	Total   int64 `json:"total"`

	Finished int64 `json:"finished"`
	Ongoing  int64 `json:"ongoing"`
	Pending  int64 `json:"pending"`

	PctOfFunnel float64 `json:"pct_of_funnel"`
	PctOfStaged float64 `json:"pct_of_staged"`

	AvgDaysInStage    *float64 `json:"avg_days_in_stage"`
	OldestDaysInStage *float64 `json:"oldest_days_in_stage"`
	Stuck             int64    `json:"stuck"`
	StuckAfterDays    int      `json:"stuck_after_days"`
	RotDaysSet        bool     `json:"rot_days_set"`
}

type StageFunnelGroup struct {
	FunnelID   string `json:"funnel_id"`
	FunnelName string `json:"funnel_name"`
	IsDefault  bool   `json:"is_default"`

	Engaged int64 `json:"engaged"`
	Shell   int64 `json:"shell"`
	Total   int64 `json:"total"`
	Stuck   int64 `json:"stuck"`

	PctOfStaged float64    `json:"pct_of_staged"`
	Stages      []StageRow `json:"stages"`
}

type OverviewStages struct {
	Funnels []StageFunnelGroup `json:"funnels"`

	StagedEngaged   int64 `json:"staged_engaged"`
	StagedShell     int64 `json:"staged_shell"`
	UnstagedEngaged int64 `json:"unstaged_engaged"`
	UnstagedShell   int64 `json:"unstaged_shell"`

	Stuck     int64 `json:"stuck"`
	Available bool  `json:"available"`
}

func BuildStageDistribution(tallies []StageTally, scopedEngaged, scopedShell int64) OverviewStages {
	out := OverviewStages{Funnels: make([]StageFunnelGroup, 0, 4)}

	byFunnel := map[string]*StageFunnelGroup{}
	order := []string{}

	for _, t := range tallies {
		g, ok := byFunnel[t.FunnelID]
		if !ok {
			g = &StageFunnelGroup{
				FunnelID:   t.FunnelID,
				FunnelName: t.FunnelName,
				IsDefault:  t.FunnelIsDefault,
				Stages:     make([]StageRow, 0, 8),
			}
			byFunnel[t.FunnelID] = g
			order = append(order, t.FunnelID)
		}

		row := StageRow{
			StageID:           t.StageID,
			StageName:         t.StageName,
			Color:             t.Color,
			Position:          t.Position,
			IsWon:             t.IsWon,
			IsLost:            t.IsLost,
			Engaged:           t.Engaged,
			Shell:             t.Shell,
			Total:             t.Engaged + t.Shell,
			Finished:          t.Finished,
			Ongoing:           t.Ongoing,
			Pending:           t.Pending,
			AvgDaysInStage:    roundDaysPtr(t.AvgOpenDays),
			OldestDaysInStage: roundDaysPtr(t.OldestOpenDays),
			Stuck:             t.Stuck,
			StuckAfterDays:    EffectiveStuckDays(t.RotDays),
			RotDaysSet:        t.RotDays != nil && *t.RotDays > 0,
		}
		g.Stages = append(g.Stages, row)
		g.Engaged += t.Engaged
		g.Shell += t.Shell
		g.Total += row.Total
		g.Stuck += t.Stuck

		out.StagedEngaged += t.Engaged
		out.StagedShell += t.Shell
		out.Stuck += t.Stuck
	}

	for _, id := range order {
		g := byFunnel[id]
		sort.SliceStable(g.Stages, func(i, j int) bool {
			if g.Stages[i].Position != g.Stages[j].Position {
				return g.Stages[i].Position < g.Stages[j].Position
			}
			return g.Stages[i].StageName < g.Stages[j].StageName
		})
		for i := range g.Stages {
			g.Stages[i].PctOfFunnel = sharePct(g.Stages[i].Engaged, g.Engaged)
			g.Stages[i].PctOfStaged = sharePct(g.Stages[i].Engaged, out.StagedEngaged)
		}
		g.PctOfStaged = sharePct(g.Engaged, out.StagedEngaged)
		out.Funnels = append(out.Funnels, *g)
	}

	sort.SliceStable(out.Funnels, func(i, j int) bool {
		a, b := out.Funnels[i], out.Funnels[j]
		if (a.FunnelID == "") != (b.FunnelID == "") {
			return b.FunnelID == ""
		}
		if a.Engaged != b.Engaged {
			return a.Engaged > b.Engaged
		}
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.FunnelName < b.FunnelName
	})

	out.UnstagedEngaged = clampNonNegative(scopedEngaged - out.StagedEngaged)
	out.UnstagedShell = clampNonNegative(scopedShell - out.StagedShell)
	out.Available = len(out.Funnels) > 0

	return out
}

func sharePct(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Round(float64(part)/float64(whole)*10000) / 100
}

func roundDaysPtr(v *float64) *float64 {
	if v == nil || *v < 0 {
		return nil
	}
	r := math.Round(*v*10) / 10
	return &r
}

func clampNonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
