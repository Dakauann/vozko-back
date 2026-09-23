package attendance

import "sort"

const (
	ReasonNoFinishedCloses = "no_finished_closes"
	ReasonNoReworkPricing  = "rework_cost_not_priced"
	ReasonReworkUnwatched  = "rework_repository_unavailable"
)

type ReworkTally struct {
	ActorID     string
	ActorKind   string
	DisplayName string
	Finished    int64
	Reopened    int64
	Templates   int64
	CostMicros  int64
}

type ReworkRow struct {
	ActorID     string   `json:"actor_id"`
	ActorKind   string   `json:"actor_kind"`
	DisplayName string   `json:"display_name"`
	Finished    int64    `json:"finished"`
	Reopened    int64    `json:"reopened"`
	ReopenRate  *float64 `json:"reopen_rate"`
	Templates   int64    `json:"templates"`
	CostMicros  int64    `json:"cost_micros"`
}

type OverviewRework struct {
	Rows          []ReworkRow `json:"rows"`
	Adjacent      []ReworkRow `json:"adjacent"`
	Team          ReworkRow   `json:"team"`
	Unassigned    ReworkRow   `json:"unassigned"`
	Currency      string      `json:"currency,omitempty"`
	CostAvailable bool        `json:"cost_available"`
	CostReason    string      `json:"cost_reason,omitempty"`
	Available     bool        `json:"available"`
	Reason        string      `json:"reason,omitempty"`
}

func UnavailableRework(reason string) OverviewRework {
	return OverviewRework{Rows: []ReworkRow{}, Adjacent: []ReworkRow{}, Reason: reason}
}

func BuildRework(tallies []ReworkTally, unassigned ReworkTally, currency string) OverviewRework {
	out := OverviewRework{
		Rows:       []ReworkRow{},
		Adjacent:   []ReworkRow{},
		Unassigned: reworkRowOf(unassigned),
		Currency:   currency,
	}

	var team ReworkTally
	team.ActorKind = ActorKindHuman

	for _, tally := range tallies {
		row := reworkRowOf(tally)
		if tally.ActorKind == ActorKindHuman {
			out.Rows = append(out.Rows, row)
			team.Finished += clampNonNegative(tally.Finished)
			team.Reopened += clampNonNegative(tally.Reopened)
			team.Templates += clampNonNegative(tally.Templates)
			team.CostMicros += clampNonNegative(tally.CostMicros)
			continue
		}
		out.Adjacent = append(out.Adjacent, row)
	}

	sortReworkRows(out.Rows)
	sortReworkRows(out.Adjacent)
	out.Team = reworkRowOf(team)

	totalFinished := team.Finished + clampNonNegative(unassigned.Finished)
	for _, row := range out.Adjacent {
		totalFinished += row.Finished
	}
	if totalFinished == 0 {
		out.Reason = ReasonNoFinishedCloses
		return out
	}
	out.Available = true

	if currency == "" {
		out.CostReason = ReasonNoReworkPricing
		return out
	}
	out.CostAvailable = true
	return out
}

func reworkRowOf(tally ReworkTally) ReworkRow {
	row := ReworkRow{
		ActorID:     tally.ActorID,
		ActorKind:   tally.ActorKind,
		DisplayName: tally.DisplayName,
		Finished:    clampNonNegative(tally.Finished),
		Reopened:    clampNonNegative(tally.Reopened),
		Templates:   clampNonNegative(tally.Templates),
		CostMicros:  clampNonNegative(tally.CostMicros),
	}
	if row.Reopened > row.Finished {
		row.Reopened = row.Finished
	}
	if row.Finished == 0 {
		return row
	}
	if rate, ok := ratioPct(float64(row.Reopened), float64(row.Finished)); ok {
		row.ReopenRate = &rate
	}
	return row
}

func sortReworkRows(rows []ReworkRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Reopened != rows[j].Reopened {
			return rows[i].Reopened > rows[j].Reopened
		}
		if (rows[i].ReopenRate == nil) != (rows[j].ReopenRate == nil) {
			return rows[i].ReopenRate != nil
		}
		if rows[i].ReopenRate != nil && *rows[i].ReopenRate != *rows[j].ReopenRate {
			return *rows[i].ReopenRate > *rows[j].ReopenRate
		}
		return rows[i].DisplayName < rows[j].DisplayName
	})
}
