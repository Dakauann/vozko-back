package launchcheck

import (
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/metaerror"
	"vozko/domain/whatsapp/template"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

func room(n int) *int { return &n }

func configured(on bool) *bool { return &on }

func healthy() Snapshot {
	return Snapshot{
		Pending:        500,
		AccessAllowed:  true,
		Template:       &TemplateFacts{Ready: true, Quality: "GREEN", Category: template.TemplateCategoryUtility, CreatedAs: template.TemplateCategoryUtility},
		Phone:          &PhoneFacts{Status: businessphone.StatusConnected},
		ExitConfigured: configured(true),
		CapRoom:        room(10000),
	}
}

func codesAt(r Report, level Level) []Code {
	var codes []Code
	for _, f := range r.Findings {
		if f.Level == level {
			codes = append(codes, f.Code)
		}
	}
	return codes
}

func has(r Report, level Level, code Code) bool {
	for _, c := range codesAt(r, level) {
		if c == code {
			return true
		}
	}
	return false
}

func TestAHealthyCampaignHasNothingToSay(t *testing.T) {
	r := Check(healthy())
	if len(r.Findings) != 0 || r.Blocks() || r.NeedsConfirmation() || r.Fingerprint() != "" {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

func TestBlocks(t *testing.T) {
	cases := map[Code]func(*Snapshot){
		CodeNothingPending:   func(s *Snapshot) { s.Pending = 0 },
		CodeAccessRevoked:    func(s *Snapshot) { s.AccessAllowed = false },
		CodeHealthUnreadable: func(s *Snapshot) { s.Unread = []string{"phone"} },
		CodeTemplateNotReady: func(s *Snapshot) { s.Template.Ready = false },
		CodePhoneUnavailable: func(s *Snapshot) { s.Phone.Status = businessphone.StatusRestricted },
		CodeMonthlyCapFull:   func(s *Snapshot) { s.CapRoom = room(0) },
	}
	for code, mutate := range cases {
		t.Run(string(code), func(t *testing.T) {
			s := healthy()
			mutate(&s)
			r := Check(s)
			if !has(r, Block, code) || !r.Blocks() {
				t.Fatalf("findings = %+v, want block %s", r.Findings, code)
			}
		})
	}
}

func TestRisksThePersonMustConfirm(t *testing.T) {
	cases := map[Code]func(*Snapshot){
		CodeTemplateRed:          func(s *Snapshot) { s.Template.Quality = "RED" },
		CodeTemplateYellow:       func(s *Snapshot) { s.Template.Quality = "YELLOW" },
		CodeTemplateFlagged:      func(s *Snapshot) { s.Template.Flagged = true },
		CodeTemplateReclassified: func(s *Snapshot) { s.Template.Category = template.TemplateCategoryMarketing },
		CodeNoExit:               func(s *Snapshot) { s.ExitConfigured = configured(false) },
		CodeLastRunFailures:      func(s *Snapshot) { s.LastRun = Run{Processed: 100, Failures: map[int]int{metaerror.Undeliverable: 20}} },
		CodeLastRunSpamLimit:     func(s *Snapshot) { s.LastRun = Run{Processed: 100, Failures: map[int]int{metaerror.SpamLimit: 1}} },
		CodeBalanceShort:         func(s *Snapshot) { s.BalanceShort = true },
	}
	for code, mutate := range cases {
		t.Run(string(code), func(t *testing.T) {
			s := healthy()
			mutate(&s)
			r := Check(s)
			if !has(r, Confirm, code) || r.Blocks() || !r.NeedsConfirmation() {
				t.Fatalf("findings = %+v, want confirm %s", r.Findings, code)
			}
		})
	}
}

func TestAdviceNeverAsksForConfirmation(t *testing.T) {
	s := healthy()
	s.LastRun = Run{Processed: 100, Failures: map[int]int{metaerror.EngagementLimit: 4, metaerror.Undeliverable: 2}}
	s.OptedOutPending = 7
	s.CapRoom = room(300)

	r := Check(s)

	for _, code := range []Code{CodeLastRunEngagement, CodeLastRunUndeliverable, CodeOptedOutPending, CodeCapBelowPending} {
		if !has(r, Advice, code) {
			t.Fatalf("findings = %+v, want advice %s", r.Findings, code)
		}
	}
	if r.NeedsConfirmation() || r.Blocks() {
		t.Fatalf("advice must not block or ask: %+v", r.Findings)
	}
}

func TestOptOutsAndHoldoutDoNotCountAsFailures(t *testing.T) {
	s := healthy()
	s.LastRun = Run{Processed: 100, Failures: map[int]int{metaerror.MarketingOptOut: 30, metaerror.ExperimentHoldout: 10}}

	if has(Check(s), Confirm, CodeLastRunFailures) {
		t.Fatal("contacts who opted out or sit in Meta's holdout are not template failures")
	}
}

func TestASmallLastRunIsNotJudged(t *testing.T) {
	s := healthy()
	s.LastRun = Run{Processed: MinSample - 1, Failures: map[int]int{metaerror.Undeliverable: MinSample - 1}}

	if has(Check(s), Confirm, CodeLastRunFailures) {
		t.Fatal("rates under the minimum sample are noise")
	}
}

func TestChannelsWithoutTemplatesOrExitsSkipThoseChecks(t *testing.T) {
	s := healthy()
	s.Template, s.Phone, s.ExitConfigured, s.CapRoom = nil, nil, nil, nil

	if r := Check(s); len(r.Findings) != 0 {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

func TestTheFingerprintFollowsTheRisksNotTheirNumbers(t *testing.T) {
	red := healthy()
	red.Template.Quality = "RED"
	redAgain := healthy()
	redAgain.Template.Quality = "RED"
	redAgain.LastRun = Run{Processed: 100, Failures: map[int]int{metaerror.EngagementLimit: 9}}
	redAndNoExit := healthy()
	redAndNoExit.Template.Quality = "RED"
	redAndNoExit.ExitConfigured = configured(false)

	first := Check(red).Fingerprint()
	if first == "" || first != Check(redAgain).Fingerprint() {
		t.Fatal("the same risks give the same fingerprint, advice aside")
	}
	if first == Check(redAndNoExit).Fingerprint() {
		t.Fatal("a new risk changes the fingerprint")
	}
	if !Check(red).Confirmed(first) || Check(redAndNoExit).Confirmed(first) {
		t.Fatal("only the current fingerprint confirms")
	}
}

func TestThresholdsAreTheStandardWavePresets(t *testing.T) {
	standard := sc.PresetPolicy(sc.Standard, nil)
	if FailureRateConfirm != standard.FailureRateStop || EngagementRateAdvice != standard.EngagementRateSlow || MinSample != standard.MinSample {
		t.Fatal("the launch check must speak the same numbers as Disparo Inteligente's Padrão")
	}
}
