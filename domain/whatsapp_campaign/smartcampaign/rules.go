package smartcampaign

import (
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/metaerror"
	"vozko/domain/whatsapp/template"
)

const (
	CodeAccessRemoved     = metaerror.AccessRemoved
	CodeUndeliverable     = metaerror.Undeliverable
	CodeAccountLocked     = metaerror.AccountLocked
	CodePaymentProblem    = metaerror.PaymentProblem
	CodeSpamLimit         = metaerror.SpamLimit
	CodeEngagementLimit   = metaerror.EngagementLimit
	CodeMarketingOptOut   = metaerror.MarketingOptOut
	CodeExperimentHoldout = metaerror.ExperimentHoldout
	CodeTemplatePaused    = metaerror.TemplatePaused
	CodeTemplateDisabled  = metaerror.TemplateDisabled
)

type Rule string

const (
	RuleSpamLimit          Rule = "spam_limit"
	RuleAccountLocked      Rule = "account_locked"
	RulePaymentProblem     Rule = "payment_problem"
	RuleAccessRemoved      Rule = "access_removed"
	RuleTemplateDisabled   Rule = "template_disabled"
	RuleTemplatePaused     Rule = "template_paused"
	RuleTemplateInReview   Rule = "template_in_review"
	RuleTemplateRed        Rule = "template_red"
	RulePhoneBanned        Rule = "phone_banned"
	RulePhoneUnavailable   Rule = "phone_unavailable"
	RuleTemplateYellow     Rule = "template_yellow"
	RulePhoneYellow        Rule = "phone_yellow"
	RulePhoneRed           Rule = "phone_red"
	RuleCategoryMarketing  Rule = "category_marketing"
	RuleTemplateFlagged    Rule = "template_flagged"
	RuleFailureRateStop    Rule = "failure_rate_stop"
	RuleFailureRateSlow    Rule = "failure_rate_slow"
	RuleEngagementLimit    Rule = "engagement_limit"
	RulePersistentSlowDown Rule = "persistent_slow_down"
	RuleHealthUnavailable  Rule = "health_unavailable"
	RuleMonthlyCap         Rule = "monthly_cap"
	RuleOutsideSendHours   Rule = "outside_send_hours"
	RuleSendHoursInvalid   Rule = "send_hours_invalid"
	RuleDailyWaveLimit     Rule = "daily_wave_limit"
)

type Finding struct {
	Rule     Rule      `json:"rule"`
	Action   Action    `json:"action"`
	Observed float64   `json:"observed,omitempty"`
	Limit    float64   `json:"limit,omitempty"`
	At       time.Time `json:"at"`
}

type codeGuard struct {
	rule Rule
	code int
}

var codeGuards = []codeGuard{
	{RuleSpamLimit, CodeSpamLimit},
	{RuleAccountLocked, CodeAccountLocked},
	{RulePaymentProblem, CodePaymentProblem},
	{RuleAccessRemoved, CodeAccessRemoved},
	{RuleTemplatePaused, CodeTemplatePaused},
	{RuleTemplateDisabled, CodeTemplateDisabled},
}

func IsSevere(code int) bool {
	for _, guard := range codeGuards {
		if guard.code == code {
			return true
		}
	}
	return false
}

func hardFindings(s Snapshot) []Finding {
	var found []Finding
	hold := func(rule Rule, action Action, at time.Time) {
		found = append(found, Finding{Rule: rule, Action: action, At: at})
	}
	for _, guard := range codeGuards {
		if n := s.Wave.Failures[guard.code]; n > 0 {
			found = append(found, Finding{Rule: guard.rule, Action: Stop, Observed: float64(n), At: s.Now})
		}
	}
	switch s.Template.Status {
	case template.TemplateStatusDisabled, template.TemplateStatusRejected:
		hold(RuleTemplateDisabled, Stop, s.Template.StatusAt)
	case template.TemplateStatusPaused:
		hold(RuleTemplatePaused, Stop, s.Template.StatusAt)
	case template.TemplateStatusPending:
		hold(RuleTemplateInReview, Pause, s.Template.StatusAt)
	}
	if s.Template.Quality == businessphone.QualityRatingRed {
		hold(RuleTemplateRed, Stop, s.Template.QualityAt)
	}
	switch {
	case s.Phone.Status == businessphone.StatusBanned:
		hold(RulePhoneBanned, Stop, s.Phone.ChangedAt)
	case s.Phone.Status.BlocksSending():
		hold(RulePhoneUnavailable, Pause, s.Phone.ChangedAt)
	}
	return found
}

func (p Policy) findings(s Snapshot) []Finding {
	var found []Finding
	if s.Template.Quality == businessphone.QualityRatingYellow {
		found = append(found, Finding{Rule: RuleTemplateYellow, Action: p.OnTemplateYellow, At: s.Template.QualityAt})
	}
	switch s.Phone.Quality {
	case businessphone.QualityRatingYellow:
		found = append(found, Finding{Rule: RulePhoneYellow, Action: p.OnPhoneYellow, At: s.Phone.QualityAt})
	case businessphone.QualityRatingRed:
		found = append(found, Finding{Rule: RulePhoneRed, Action: p.OnPhoneRed, At: s.Phone.QualityAt})
	}
	if s.Template.Reclassified() {
		found = append(found, Finding{Rule: RuleCategoryMarketing, Action: Pause, At: s.Template.CategoryAt})
	}
	if s.Template.Flagged {
		found = append(found, Finding{Rule: RuleTemplateFlagged, Action: Pause, At: s.Template.StatusAt})
	}
	return append(found, p.rateFindings(s.Wave, s.Now)...)
}

func (p Policy) rateFindings(o Outcome, now time.Time) []Finding {
	if o.Judged() < p.MinSample {
		return nil
	}
	var found []Finding
	switch failure := o.FailureRate(); {
	case lowerBound(o.TemplateFailures(), o.Judged()) >= p.FailureRateStop:
		found = append(found, Finding{Rule: RuleFailureRateStop, Action: Stop, Observed: failure, Limit: p.FailureRateStop, At: now})
	case failure >= p.FailureRateSlow:
		found = append(found, Finding{Rule: RuleFailureRateSlow, Action: SlowDown, Observed: failure, Limit: p.FailureRateSlow, At: now})
	}
	if held := o.Share(o.Failures[CodeEngagementLimit]); held >= p.EngagementRateSlow {
		found = append(found, Finding{Rule: RuleEngagementLimit, Action: SlowDown, Observed: held, Limit: p.EngagementRateSlow, At: now})
	}
	return found
}
