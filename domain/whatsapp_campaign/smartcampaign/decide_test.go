package smartcampaign

import (
	"errors"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	"vozko/domain/working_hours"
)

var brasilia = mustLocation("America/Sao_Paulo")

var noon = time.Date(2026, 10, 16, 12, 0, 0, 0, brasilia)

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func businessHours() *working_hours.Spec {
	open := []working_hours.Window{{Start: "08:00", End: "20:00"}}
	return &working_hours.Spec{
		Timezone: "America/Sao_Paulo",
		Days:     map[string][]working_hours.Window{"mon": open, "tue": open, "wed": open, "thu": open, "fri": open, "sat": open},
	}
}

func standardPolicy() Policy {
	return PresetPolicy(Standard, businessHours())
}

func healthySnapshot() Snapshot {
	return Snapshot{
		Now: noon,
		Template: TemplateState{
			Status:    template.TemplateStatusApproved,
			Quality:   businessphone.QualityRatingGreen,
			Category:  template.TemplateCategoryUtility,
			CreatedAs: template.TemplateCategoryUtility,
		},
		Phone:   PhoneState{Status: businessphone.StatusConnected},
		Pending: 5000,
		CapRoom: 100000,
	}
}

func outcome(processed int, failures map[int]int) Outcome {
	o := Outcome{Processed: processed, Failures: failures}
	o.Delivered = processed - o.Failed()
	return o
}

func hasRule(d Decision, rule Rule) bool {
	for _, f := range d.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name     string
		policy   func(*Policy)
		snapshot func(*Snapshot)
		history  History
		moment   Moment
		action   Action
		rule     Rule
		size     int
	}{
		{name: "first wave starts at the first size", moment: BeforeNextWave, action: Continue, size: 200},
		{name: "a healthy wave grows the next one", history: History{CurrentSize: 400}, moment: BeforeNextWave, action: Continue, size: 800,
			snapshot: func(s *Snapshot) { s.Wave = outcome(400, map[int]int{CodeUndeliverable: 6}) }},
		{name: "growth stops at the max size", history: History{CurrentSize: 4000}, moment: BeforeNextWave, action: Continue, size: 5000},
		{name: "the next wave never exceeds what is left", history: History{CurrentSize: 400}, moment: BeforeNextWave, action: Continue, size: 300,
			snapshot: func(s *Snapshot) { s.Pending = 300 }},
		{name: "the next wave never exceeds the monthly room", history: History{CurrentSize: 400}, moment: BeforeNextWave, action: Continue, size: 120,
			snapshot: func(s *Snapshot) { s.CapRoom = 120 }},
		{name: "failure rate in the slow band halves the wave", history: History{CurrentSize: 400}, moment: BeforeNextWave, action: SlowDown, rule: RuleFailureRateSlow, size: 200,
			snapshot: func(s *Snapshot) { s.Wave = outcome(400, map[int]int{CodeUndeliverable: 40}) }},
		{name: "slowing down never goes under the floor", history: History{CurrentSize: 60}, moment: BeforeNextWave, action: SlowDown, size: 50,
			snapshot: func(s *Snapshot) { s.Wave = outcome(60, map[int]int{CodeUndeliverable: 6}) }},
		{name: "engagement limit slows the next wave", history: History{CurrentSize: 400}, moment: BeforeNextWave, action: SlowDown, rule: RuleEngagementLimit, size: 200,
			snapshot: func(s *Snapshot) { s.Wave = outcome(400, map[int]int{CodeEngagementLimit: 20}) }},
		{name: "a failure rate clearly above stop stops during the wave", moment: DuringWave, action: Stop, rule: RuleFailureRateStop,
			snapshot: func(s *Snapshot) { s.Wave = outcome(100, map[int]int{CodeUndeliverable: 30}) }},
		{name: "a small wave just over stop is not enough to stop", history: History{CurrentSize: 50}, moment: BeforeNextWave, action: SlowDown, rule: RuleFailureRateSlow,
			snapshot: func(s *Snapshot) { s.Wave = outcome(50, map[int]int{CodeUndeliverable: 9}) }},
		{name: "rates wait for the minimum sample", moment: DuringWave, action: Continue,
			snapshot: func(s *Snapshot) { s.Wave = outcome(10, map[int]int{CodeUndeliverable: 5}) }},
		{name: "one spam limit error stops at once", moment: DuringWave, action: Stop, rule: RuleSpamLimit,
			snapshot: func(s *Snapshot) { s.Wave = outcome(3, map[int]int{CodeSpamLimit: 1}) }},
		{name: "a locked account stops at once", moment: DuringWave, action: Stop, rule: RuleAccountLocked,
			snapshot: func(s *Snapshot) { s.Wave = outcome(2, map[int]int{CodeAccountLocked: 1}) }},
		{name: "a payment problem stops at once", moment: DuringWave, action: Stop, rule: RulePaymentProblem,
			snapshot: func(s *Snapshot) { s.Wave = outcome(2, map[int]int{CodePaymentProblem: 1}) }},
		{name: "removed access stops at once", moment: DuringWave, action: Stop, rule: RuleAccessRemoved,
			snapshot: func(s *Snapshot) { s.Wave = outcome(2, map[int]int{CodeAccessRemoved: 1}) }},
		{name: "a slow down is not acted on during a wave", moment: DuringWave, action: Continue,
			snapshot: func(s *Snapshot) { s.Wave = outcome(400, map[int]int{CodeEngagementLimit: 20}) }},
		{name: "a slow down stuck at the floor pauses", history: History{CurrentSize: 50, SlowStreakAtFloor: 2}, moment: BeforeNextWave, action: Pause, rule: RulePersistentSlowDown,
			policy:   func(p *Policy) { p.OnTemplateYellow = SlowDown },
			snapshot: func(s *Snapshot) { s.Template.Quality = businessphone.QualityRatingYellow }},
		{name: "a slow down above the floor keeps shrinking", history: History{CurrentSize: 400, SlowStreakAtFloor: 2}, moment: BeforeNextWave, action: SlowDown, size: 200,
			policy:   func(p *Policy) { p.OnTemplateYellow = SlowDown },
			snapshot: func(s *Snapshot) { s.Template.Quality = businessphone.QualityRatingYellow }},
		{name: "a yellow template pauses on the standard preset", moment: DuringWave, action: Pause, rule: RuleTemplateYellow,
			snapshot: func(s *Snapshot) { s.Template.Quality = businessphone.QualityRatingYellow }},
		{name: "a yellow template slows down on the aggressive preset", history: History{CurrentSize: 500}, moment: BeforeNextWave, action: SlowDown, rule: RuleTemplateYellow, size: 250,
			policy:   func(p *Policy) { *p = PresetPolicy(Aggressive, businessHours()) },
			snapshot: func(s *Snapshot) { s.Template.Quality = businessphone.QualityRatingYellow }},
		{name: "a red template stops", moment: DuringWave, action: Stop, rule: RuleTemplateRed,
			snapshot: func(s *Snapshot) { s.Template.Quality = businessphone.QualityRatingRed }},
		{name: "a template paused by Meta stops", moment: DuringWave, action: Stop, rule: RuleTemplatePaused,
			snapshot: func(s *Snapshot) { s.Template.Status = template.TemplateStatusPaused }},
		{name: "a disabled template stops", moment: DuringWave, action: Stop, rule: RuleTemplateDisabled,
			snapshot: func(s *Snapshot) { s.Template.Status = template.TemplateStatusDisabled }},
		{name: "a template reclassified as marketing pauses", moment: DuringWave, action: Pause, rule: RuleCategoryMarketing,
			snapshot: func(s *Snapshot) { s.Template.Category = template.TemplateCategoryMarketing }},
		{name: "a campaign created as marketing is not reclassified", moment: DuringWave, action: Continue,
			snapshot: func(s *Snapshot) {
				s.Template.Category = template.TemplateCategoryMarketing
				s.Template.CreatedAs = template.TemplateCategoryMarketing
			}},
		{name: "a banned number stops", moment: DuringWave, action: Stop, rule: RulePhoneBanned,
			snapshot: func(s *Snapshot) { s.Phone.Status = businessphone.StatusBanned }},
		{name: "a restricted number pauses", moment: DuringWave, action: Pause, rule: RulePhoneUnavailable,
			snapshot: func(s *Snapshot) { s.Phone.Status = businessphone.StatusRestricted }},
		{name: "a disconnected number pauses", moment: DuringWave, action: Pause, rule: RulePhoneUnavailable,
			snapshot: func(s *Snapshot) { s.Phone.Status = businessphone.StatusDisconnected }},
		{name: "a template Meta flagged as at risk of being disabled pauses", moment: DuringWave, action: Pause, rule: RuleTemplateFlagged,
			snapshot: func(s *Snapshot) { s.Template.Flagged = true }},
		{name: "a template paused by Meta mid wave (132015) stops at once", moment: DuringWave, action: Stop, rule: RuleTemplatePaused,
			snapshot: func(s *Snapshot) { s.Wave = outcome(40, map[int]int{CodeTemplatePaused: 1}) }},
		{name: "a template Meta disabled for good (132016) stops at once", moment: DuringWave, action: Stop, rule: RuleTemplateDisabled,
			snapshot: func(s *Snapshot) { s.Wave = outcome(40, map[int]int{CodeTemplateDisabled: 1}) }},
		{name: "contacts who opted out or sit in Meta's holdout are not template failures", history: History{CurrentSize: 400}, moment: BeforeNextWave, action: Continue, size: 800,
			snapshot: func(s *Snapshot) {
				s.Wave = outcome(400, map[int]int{CodeMarketingOptOut: 60, CodeExperimentHoldout: 20})
			}},
		{name: "unreadable health waits", moment: BeforeNextWave, action: Wait, rule: RuleHealthUnavailable,
			history:  History{UnreadSince: noon.Add(-30 * time.Minute)},
			snapshot: func(s *Snapshot) { s.Unread = []Signal{SignalPhone} }},
		{name: "unreadable health past the max observe wait pauses", moment: BeforeNextWave, action: Pause, rule: RuleHealthUnavailable,
			history:  History{UnreadSince: noon.Add(-6 * time.Hour)},
			snapshot: func(s *Snapshot) { s.Unread = []Signal{SignalPhone} }},
		{name: "a known stop signal still stops while health is unreadable", moment: BeforeNextWave, action: Stop, rule: RuleSpamLimit,
			snapshot: func(s *Snapshot) {
				s.Unread = []Signal{SignalTemplate}
				s.Wave = outcome(200, map[int]int{CodeSpamLimit: 1})
			}},
		{name: "nothing left completes", moment: BeforeNextWave, action: Complete,
			snapshot: func(s *Snapshot) { s.Pending = 0 }},
		{name: "no monthly room pauses", moment: BeforeNextWave, action: Pause, rule: RuleMonthlyCap,
			snapshot: func(s *Snapshot) { s.CapRoom = 0 }},
		{name: "outside send hours waits", moment: BeforeNextWave, action: Wait, rule: RuleOutsideSendHours,
			snapshot: func(s *Snapshot) { s.Now = time.Date(2026, 10, 16, 22, 30, 0, 0, brasilia) }},
		{name: "a closed day waits", moment: BeforeNextWave, action: Wait, rule: RuleOutsideSendHours,
			snapshot: func(s *Snapshot) { s.Now = time.Date(2026, 10, 18, 12, 0, 0, 0, brasilia) }},
		{name: "send hours that do not compile pause", moment: BeforeNextWave, action: Pause, rule: RuleSendHoursInvalid,
			policy: func(p *Policy) {
				p.SendHours = &working_hours.Spec{Timezone: "Mars/Olympus", Days: businessHours().Days}
			}},
		{name: "the daily wave limit waits", moment: BeforeNextWave, action: Wait, rule: RuleDailyWaveLimit,
			policy:   func(p *Policy) { p.MaxWavesPerDay = 3 },
			snapshot: func(s *Snapshot) { s.WavesToday = 3 }},
		{name: "a resume restarts at the minimum size", history: History{CurrentSize: 1600, ResumedAfterHold: true}, moment: BeforeNextWave, action: Continue, size: 50},
		{name: "a wave interrupted by a person is repeated at its size", history: History{CurrentSize: 800, Interrupted: true}, moment: BeforeNextWave, action: Continue, size: 800},
		{name: "unread health is not judged during a wave", moment: DuringWave, action: Continue,
			snapshot: func(s *Snapshot) { s.Unread = []Signal{SignalTemplate} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := standardPolicy()
			if tc.policy != nil {
				tc.policy(&policy)
			}
			snapshot := healthySnapshot()
			if tc.snapshot != nil {
				tc.snapshot(&snapshot)
			}
			d := Decide(policy, snapshot, tc.history, tc.moment)
			if d.Action != tc.action {
				t.Fatalf("action = %s, want %s (findings %+v)", d.Action, tc.action, d.Findings)
			}
			if tc.rule != "" && !hasRule(d, tc.rule) {
				t.Fatalf("missing rule %s in %+v", tc.rule, d.Findings)
			}
			if tc.size != 0 && d.NextSize != tc.size {
				t.Fatalf("next size = %d, want %d", d.NextSize, tc.size)
			}
		})
	}
}

func TestOutsideSendHoursWaitsUntilTheNextOpening(t *testing.T) {
	snapshot := healthySnapshot()
	snapshot.Now = time.Date(2026, 10, 17, 21, 0, 0, 0, brasilia)
	d := Decide(standardPolicy(), snapshot, History{}, BeforeNextWave)
	monday := time.Date(2026, 10, 19, 8, 0, 0, 0, brasilia)
	if !d.WaitUntil.Equal(monday) {
		t.Fatalf("waits until %v, want %v", d.WaitUntil, monday)
	}
}

func TestPresetsAreValid(t *testing.T) {
	for _, preset := range Presets {
		if err := PresetPolicy(preset, businessHours()).Validate(); err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
	}
}

func TestUnknownPresetIsRefused(t *testing.T) {
	if _, err := ParsePreset("turbo"); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("ParsePreset(turbo) = %v, want ErrInvalidPolicy", err)
	}
}

func TestValidateRefusesUnsafePolicies(t *testing.T) {
	cases := map[string]func(*Policy){
		"send hours are required":               func(p *Policy) { p.SendHours = nil },
		"send hours must open at some point":    func(p *Policy) { p.SendHours = &working_hours.Spec{Timezone: "America/Sao_Paulo"} },
		"a yellow template may not continue":    func(p *Policy) { p.OnTemplateYellow = Continue },
		"slow must stay under stop":             func(p *Policy) { p.FailureRateSlow = 0.2 },
		"the min wave may not exceed the first": func(p *Policy) { p.MinWaveSize = 500 },
		"max observe may not undercut observe":  func(p *Policy) { p.MaxObserveWait = time.Hour },
		"the min sample keeps rates meaningful": func(p *Policy) { p.MinSample = 5 },
		"a stuck slow down must end":            func(p *Policy) { p.MaxSlowWavesAtFloor = 0 },
		"waves per day cannot be negative":      func(p *Policy) { p.MaxWavesPerDay = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			policy := standardPolicy()
			mutate(&policy)
			if err := policy.Validate(); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("Validate() = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestLowerBoundStaysUnderTheShareAndTightensWithMoreData(t *testing.T) {
	small, large := lowerBound(9, 50), lowerBound(180, 1000)
	if small >= 0.18 || large >= 0.18 || small >= large {
		t.Fatalf("lower bounds %.3f (50) and %.3f (1000) must stay under 18%% and tighten with more data", small, large)
	}
	if lowerBound(0, 0) != 0 {
		t.Fatal("no data must give a zero bound")
	}
}

func TestSevereCodesAreTheGuardedOnes(t *testing.T) {
	if !IsSevere(CodeSpamLimit) || !IsSevere(CodeTemplatePaused) || !IsSevere(CodeTemplateDisabled) || IsSevere(CodeUndeliverable) {
		t.Fatal("only codes with a hard rule are severe")
	}
}

func TestDriversAreTheFindingsBehindTheAction(t *testing.T) {
	snapshot := healthySnapshot()
	snapshot.Template.Quality = businessphone.QualityRatingYellow
	snapshot.Phone.Status = businessphone.StatusBanned
	d := Decide(standardPolicy(), snapshot, History{}, DuringWave)
	if d.Action != Stop || len(d.Drivers()) != 2 {
		t.Fatalf("action %s with drivers %+v, want stop driven by both holds", d.Action, d.Drivers())
	}
	primary, _ := d.Primary()
	if primary.Rule != RulePhoneBanned {
		t.Fatalf("primary = %s, want the strongest finding", primary.Rule)
	}
}

func TestTheDailyLimitWaitsForTheNextDayOpening(t *testing.T) {
	policy := standardPolicy()
	policy.MaxWavesPerDay = 2
	snapshot := healthySnapshot()
	snapshot.WavesToday = 2
	d := Decide(policy, snapshot, History{}, BeforeNextWave)
	saturday := time.Date(2026, 10, 17, 8, 0, 0, 0, brasilia)
	if d.Action != Wait || !d.WaitUntil.Equal(saturday) {
		t.Fatalf("%s until %v, want wait until %v", d.Action, d.WaitUntil, saturday)
	}
}

func TestDayStartFollowsTheSendHoursTimezone(t *testing.T) {
	lateUTC := time.Date(2026, 10, 17, 1, 30, 0, 0, time.UTC)
	want := time.Date(2026, 10, 16, 0, 0, 0, 0, brasilia)
	if got := standardPolicy().DayStart(lateUTC); !got.Equal(want) {
		t.Fatalf("day start = %v, want %v", got, want)
	}
}
