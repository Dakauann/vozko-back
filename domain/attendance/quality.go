package attendance

import (
	"sort"
	"time"
)

const (
	ReasonCaptureDisabled  = "outcome_capture_disabled"
	ReasonCaptureNotDated  = "outcome_capture_has_no_enabled_at"
	ReasonNothingCaptured  = "nothing_captured_yet"
	ReasonQualityUnwatched = "quality_repository_unavailable"
)

type QualityTally struct {
	ActorID     string
	ActorKind   string
	DisplayName string
	Closes      int64
	Captured    int64
	Durable     int64
}

type QualityRow struct {
	ActorID     string   `json:"actor_id"`
	ActorKind   string   `json:"actor_kind"`
	DisplayName string   `json:"display_name"`
	Closes      int64    `json:"closes"`
	Captured    int64    `json:"captured"`
	Durable     int64    `json:"durable"`
	DurablePct  *float64 `json:"durable_pct"`
	Verdict     Verdict  `json:"verdict"`
}

type Quality struct {
	Threshold   float64      `json:"threshold"`
	Rows        []QualityRow `json:"rows"`
	Adjacent    []QualityRow `json:"adjacent"`
	Team        QualityRow   `json:"team"`
	EnabledAt   *time.Time   `json:"enabled_at"`
	NotCaptured int64        `json:"not_captured"`
	Available   bool         `json:"available"`
	Reason      string       `json:"reason,omitempty"`
}

func UnavailableQuality(reason string) Quality {
	return Quality{Rows: []QualityRow{}, Adjacent: []QualityRow{}, Reason: reason}
}

func BuildQuality(tallies []QualityTally, threshold float64, enabledAt *time.Time, notCaptured int64) Quality {
	out := Quality{
		Threshold:   threshold,
		Rows:        []QualityRow{},
		Adjacent:    []QualityRow{},
		EnabledAt:   enabledAt,
		NotCaptured: clampNonNegative(notCaptured),
	}
	if enabledAt == nil {
		out.Reason = ReasonCaptureNotDated
		return out
	}

	var teamCloses, teamCaptured, teamDurable int64
	for _, t := range tallies {
		row := qualityRowOf(t, threshold)
		if t.ActorKind == ActorKindHuman {
			out.Rows = append(out.Rows, row)
			teamCloses += clampNonNegative(t.Closes)
			teamCaptured += clampNonNegative(t.Captured)
			teamDurable += clampNonNegative(t.Durable)
			continue
		}
		row.Verdict = ""
		out.Adjacent = append(out.Adjacent, row)
	}

	sortQualityRows(out.Rows)
	sortQualityRows(out.Adjacent)

	out.Team = qualityRowOf(QualityTally{
		ActorKind: ActorKindHuman,
		Closes:    teamCloses,
		Captured:  teamCaptured,
		Durable:   teamDurable,
	}, threshold)

	if teamCaptured == 0 {
		out.Reason = ReasonNothingCaptured
		return out
	}
	out.Available = true
	return out
}

func qualityRowOf(t QualityTally, threshold float64) QualityRow {
	row := QualityRow{
		ActorID:     t.ActorID,
		ActorKind:   t.ActorKind,
		DisplayName: t.DisplayName,
		Closes:      clampNonNegative(t.Closes),
		Captured:    clampNonNegative(t.Captured),
		Durable:     clampNonNegative(t.Durable),
		Verdict:     VerdictInsufficientData,
	}
	if row.Durable > row.Captured {
		row.Durable = row.Captured
	}
	if row.Captured == 0 {
		return row
	}
	pct, ok := ratioPct(float64(row.Durable), float64(row.Captured))
	if !ok {
		return row
	}
	row.DurablePct = &pct
	row.Verdict = verdictFor(DirectionHigherIsBetter, pct, threshold)
	return row
}

func sortQualityRows(rows []QualityRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if (left.DurablePct == nil) != (right.DurablePct == nil) {
			return left.DurablePct != nil
		}
		if left.DurablePct != nil && *left.DurablePct != *right.DurablePct {
			return *left.DurablePct > *right.DurablePct
		}
		if left.Captured != right.Captured {
			return left.Captured > right.Captured
		}
		return left.DisplayName < right.DisplayName
	})
}
