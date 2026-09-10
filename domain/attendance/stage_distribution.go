package attendance

import (
	"math"
	"sort"
)

// DefaultStageStuckDays is the fallback rot threshold for a stage that defines
// no rot_days of its own.
//
// It matches the 7-day default already used by the CRM's "Parada Nd" chip
// (vozko-front/src/lib/crm/opportunities.ts, rotSignal). One conversation must
// not read as stalled on the board and healthy on this panel, which is exactly
// what a second, independently chosen number would produce.
const DefaultStageStuckDays = 7

// EffectiveStuckDays resolves the threshold a stage is measured against.
//
// Zero and negatives are treated as "unset" rather than obeyed: a zero
// threshold marks every open conversation stuck the moment it arrives, which
// reports a workspace-wide emergency caused by a blank form field.
func EffectiveStuckDays(rotDays *int) int {
	if rotDays != nil && *rotDays > 0 {
		return *rotDays
	}
	return DefaultStageStuckDays
}

// StageTally is one (funnel, stage) row exactly as the store returns it, before
// any grouping. The repository fills these from a single GROUP BY; every shape
// decision below is pure and therefore testable without a database.
type StageTally struct {
	// FunnelID is empty for a stage that belongs to no pipeline. Those are real
	// (campaign-scoped stages predate funnels) and get their own bucket.
	FunnelID        string
	FunnelName      string
	FunnelIsDefault bool

	StageID   string
	StageName string
	Color     string
	Position  int
	IsWon     bool
	IsLost    bool
	// RotDays is the stage's own stuck threshold, nil when it defines none.
	RotDays *int

	// Engaged and Shell partition the scoped conversations sitting in the stage.
	Engaged int64
	Shell   int64
	// Status split of the ENGAGED rows only.
	Finished int64
	Ongoing  int64
	Pending  int64

	// Dwell over OPEN engaged conversations, nil when the stage holds none.
	AvgOpenDays    *float64
	OldestOpenDays *float64
	// Stuck is open engaged conversations past this stage's effective threshold.
	Stuck int64
}

// StageRow is one stage's share of the period, as the panel renders it.
type StageRow struct {
	StageID   string `json:"stage_id"`
	StageName string `json:"stage_name"`
	// Color is the stage's own kanban colour when the workspace picked one.
	Color    string `json:"color,omitempty"`
	Position int    `json:"position"`
	IsWon    bool   `json:"is_won"`
	IsLost   bool   `json:"is_lost"`

	// Engaged is THE number: conversations with at least one message, the same
	// universe as the KPI strip and every other panel on the page.
	Engaged int64 `json:"engaged"`
	// Shell is campaign/import rows parked here that were never messaged. Shown
	// beside Engaged, never added to it.
	Shell int64 `json:"shell"`
	Total int64 `json:"total"`

	Finished int64 `json:"finished"`
	Ongoing  int64 `json:"ongoing"`
	Pending  int64 `json:"pending"`

	// PctOfFunnel answers "where inside this path do they stop", PctOfStaged
	// answers "which funnel owns the workspace's volume". Both over Engaged.
	PctOfFunnel float64 `json:"pct_of_funnel"`
	PctOfStaged float64 `json:"pct_of_staged"`

	// AvgDaysInStage and OldestDaysInStage measure OPEN engaged conversations
	// against now, not against the period end. Null when nothing here is open,
	// because 0 would read as "everyone arrived today".
	AvgDaysInStage    *float64 `json:"avg_days_in_stage"`
	OldestDaysInStage *float64 `json:"oldest_days_in_stage"`
	// Stuck is open engaged conversations sitting longer than StuckAfterDays.
	Stuck          int64 `json:"stuck"`
	StuckAfterDays int   `json:"stuck_after_days"`
	// RotDaysSet distinguishes a threshold the workspace chose from the product
	// default, so the UI can name which one produced the count.
	RotDaysSet bool `json:"rot_days_set"`
}

// StageFunnelGroup is one funnel (pipeline) with the stages it owns.
//
// The grouping is the point. Five production workspaces carry more than one
// conversation funnel, and duplicate stage names across them are the normal
// case, so a flat list would add "Agendamento" from a dead funnel to
// "Agendamento" from the live one and report a number belonging to neither.
type StageFunnelGroup struct {
	// FunnelID is empty for the single no-funnel bucket, which always sorts last.
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

// OverviewStages is the funnel-grouped stage distribution for the period.
type OverviewStages struct {
	Funnels []StageFunnelGroup `json:"funnels"`

	StagedEngaged int64 `json:"staged_engaged"`
	StagedShell   int64 `json:"staged_shell"`
	// Unstaged is scoped minus staged: conversations carrying no stage at all.
	// Reported as a total rather than as a fake stage row.
	UnstagedEngaged int64 `json:"unstaged_engaged"`
	UnstagedShell   int64 `json:"unstaged_shell"`

	Stuck     int64 `json:"stuck"`
	Available bool  `json:"available"` // true when anything in scope is staged
}

// BuildStageDistribution groups flat store tallies into funnels and computes
// every derived number the panel shows.
//
// scopedEngaged and scopedShell are the period's totals from the KPI pass, so
// "unstaged" costs no query: it is the difference between what the period
// scoped and what the funnels hold.
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

	// Busiest funnel first, so the panel opens on where the work actually is.
	// The no-funnel bucket sorts last regardless of size: it is a leftover, not
	// a funnel, and heading the panel with it would misname the workspace's
	// biggest pipeline.
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

// sharePct is a percentage of a whole that may legitimately be zero (a funnel
// holding only campaign shells). NaN here would serialise as null and break the
// bar it sizes.
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
