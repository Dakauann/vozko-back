package studio

import "slices"

const (
	MinGainDB        = -60.0
	MaxGainDB        = 6.0
	MinDuckDB        = 1.0
	MaxDuckDB        = 40.0
	MaxDuckAttackMS  = 2000
	MaxDuckReleaseMS = 3000
	MaxDuckHoldMS    = 2000
)

type DuckingRule struct {
	TriggerTrackIDs []string `json:"triggerTrackIds"`
	ReduceDB        float64  `json:"reduceDb"`
	AttackMS        int64    `json:"attackMs"`
	ReleaseMS       int64    `json:"releaseMs"`
	HoldMS          int64    `json:"holdMs"`
	Sensitivity     float64  `json:"sensitivity"`
}

func (r *DuckingRule) rangeIssue() string {
	switch {
	case !within(r.ReduceDB, MinDuckDB, MaxDuckDB) || !within(r.Sensitivity, 0, 1):
		return CodeOutOfRange
	case r.AttackMS < 0 || r.AttackMS > MaxDuckAttackMS || r.ReleaseMS < 0 || r.ReleaseMS > MaxDuckReleaseMS || r.HoldMS < 0 || r.HoldMS > MaxDuckHoldMS:
		return CodeOutOfRange
	}
	return ""
}

func (d VideoDocument) trackByID(id string) *Track {
	for i := range d.Tracks {
		if d.Tracks[i].ID == id {
			return &d.Tracks[i]
		}
	}
	return nil
}

func (d VideoDocument) ducksUnder(trackID, target string, seen map[string]bool) bool {
	track := d.trackByID(trackID)
	if track == nil || track.Ducking == nil {
		return false
	}
	for _, id := range track.Ducking.TriggerTrackIDs {
		if id == target {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if d.ducksUnder(id, target, seen) {
			return true
		}
	}
	return false
}

func (d VideoDocument) trackSoundIssue(track Track) string {
	if track.GainDB != nil && (track.Kind != TrackAudio || !within(*track.GainDB, MinGainDB, MaxGainDB)) {
		if track.Kind != TrackAudio {
			return CodeInvalid
		}
		return CodeOutOfRange
	}
	if code := track.panIssue(); code != "" {
		return code
	}
	rule := track.Ducking
	if rule == nil {
		return ""
	}
	if track.Kind != TrackAudio || len(rule.TriggerTrackIDs) == 0 {
		return CodeInvalid
	}
	for i, id := range rule.TriggerTrackIDs {
		trigger := d.trackByID(id)
		if id == track.ID || trigger == nil || trigger.Kind != TrackAudio || slices.Contains(rule.TriggerTrackIDs[:i], id) {
			return CodeInvalid
		}
	}
	if code := rule.rangeIssue(); code != "" {
		return code
	}
	if d.ducksUnder(track.ID, track.ID, map[string]bool{}) {
		return CodeInvalid
	}
	return ""
}
