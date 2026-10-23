package smartcampaign

import (
	"time"

	"vozko/domain/working_hours"
)

type Moment int

const (
	DuringWave Moment = iota
	BeforeNextWave
)

type History struct {
	CurrentSize       int
	SlowStreakAtFloor int
	Interrupted       bool
	ResumedAfterHold  bool
	UnreadSince       time.Time
}

type Decision struct {
	Action    Action    `json:"action"`
	Findings  []Finding `json:"findings,omitempty"`
	NextSize  int       `json:"nextSize,omitempty"`
	WaitUntil time.Time `json:"waitUntil,omitzero"`
}

func (d Decision) Primary() (Finding, bool) {
	for _, f := range d.Findings {
		if f.Action == d.Action {
			return f, true
		}
	}
	return Finding{}, false
}

func (d Decision) Drivers() []Finding {
	var drivers []Finding
	for _, f := range d.Findings {
		if f.Action == d.Action || (d.Action.Holds() && f.Action.Holds()) {
			drivers = append(drivers, f)
		}
	}
	return drivers
}

func Decide(p Policy, s Snapshot, h History, m Moment) Decision {
	findings := append(hardFindings(s), p.findings(s)...)
	if action := Strongest(findings); action.Holds() {
		return Decision{Action: action, Findings: findings}
	}
	if m == DuringWave {
		return Decision{Action: Continue, Findings: findings}
	}
	if s.Pending == 0 {
		return Decision{Action: Complete, Findings: findings}
	}
	action := Strongest(findings)
	if action == SlowDown && p.stuckAtFloor(h) {
		stuck := Finding{Rule: RulePersistentSlowDown, Action: Pause, Observed: float64(h.SlowStreakAtFloor + 1), Limit: float64(p.MaxSlowWavesAtFloor), At: s.Now}
		return Decision{Action: Pause, Findings: append([]Finding{stuck}, findings...)}
	}
	if len(s.Unread) > 0 {
		return awaitHealth(p, s, h, findings)
	}
	if s.CapRoom <= 0 {
		return Decision{Action: Pause, Findings: append(findings, Finding{Rule: RuleMonthlyCap, Action: Pause, At: s.Now})}
	}
	hours, err := p.SendHours.Compile()
	if err != nil || hours == nil {
		return Decision{Action: Pause, Findings: append(findings, Finding{Rule: RuleSendHoursInvalid, Action: Pause, At: s.Now})}
	}
	if !hours.IsOpen(s.Now) {
		opens, _ := hours.NextOpen(s.Now)
		return Decision{Action: Wait, WaitUntil: opens, Findings: append(findings, Finding{Rule: RuleOutsideSendHours, Action: Wait, At: s.Now})}
	}
	if p.MaxWavesPerDay > 0 && s.WavesToday >= p.MaxWavesPerDay {
		limit := Finding{Rule: RuleDailyWaveLimit, Action: Wait, Observed: float64(s.WavesToday), Limit: float64(p.MaxWavesPerDay), At: s.Now}
		return Decision{Action: Wait, WaitUntil: nextDayOpening(hours, s.Now), Findings: append(findings, limit)}
	}
	size := min(p.nextSize(h, action == SlowDown), s.Pending, s.CapRoom)
	return Decision{Action: action, Findings: findings, NextSize: size}
}

func awaitHealth(p Policy, s Snapshot, h History, findings []Finding) Decision {
	unreadFor := s.Now.Sub(h.UnreadSince)
	unavailable := Finding{Rule: RuleHealthUnavailable, Action: Wait, Observed: unreadFor.Minutes(), Limit: p.MaxObserveWait.Minutes(), At: s.Now}
	if !h.UnreadSince.IsZero() && unreadFor >= p.MaxObserveWait {
		unavailable.Action = Pause
	}
	return Decision{Action: unavailable.Action, Findings: append(findings, unavailable)}
}

func nextDayOpening(hours *working_hours.Schedule, now time.Time) time.Time {
	local := now.In(hours.Location())
	tomorrow := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, hours.Location())
	opens, _ := hours.NextOpen(tomorrow)
	return opens
}
