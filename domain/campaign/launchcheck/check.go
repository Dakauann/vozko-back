package launchcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/metaerror"
	"vozko/domain/whatsapp/template"
)

type Level string

const (
	Block   Level = "block"
	Confirm Level = "confirm"
	Advice  Level = "advice"
)

type Code string

const (
	CodeNothingPending       Code = "nothing_pending"
	CodeAudienceEmpty        Code = "audience_empty"
	CodeAccessRevoked        Code = "access_revoked"
	CodeHealthUnreadable     Code = "health_unreadable"
	CodePlanInactive         Code = "plan_inactive"
	CodeTemplateMissing      Code = "template_missing"
	CodeTemplateNotReady     Code = "template_not_ready"
	CodePhoneUnavailable     Code = "phone_unavailable"
	CodeMonthlyCapFull       Code = "monthly_cap_full"
	CodeTemplateRed          Code = "template_red"
	CodeTemplateYellow       Code = "template_yellow"
	CodeTemplateFlagged      Code = "template_flagged"
	CodeTemplateReclassified Code = "template_reclassified"
	CodeNoExit               Code = "no_exit"
	CodeLastRunFailures      Code = "last_run_failures"
	CodeLastRunSpamLimit     Code = "last_run_spam_limit"
	CodeBalanceShort         Code = "balance_short"
	CodeLastRunEngagement    Code = "last_run_engagement"
	CodeLastRunUndeliverable Code = "last_run_undeliverable"
	CodeOptedOutPending      Code = "opted_out_pending"
	CodeCapBelowPending      Code = "cap_below_pending"
	CodeFormNotSendable      Code = "form_not_sendable"
	CodeNumberUnusable       Code = "number_unusable"
	CodeNumberRestricted     Code = "number_restricted"
	CodeNumberCannotSend     Code = "number_cannot_send"
	CodeGroupsUnsendable     Code = "groups_unsendable"
)

const (
	FailureRateConfirm   = 0.15
	EngagementRateAdvice = 0.03
	MinSample            = 50
)

type TemplateFacts struct {
	Ready           bool
	Quality         string
	Flagged         bool
	Category        template.TemplateCategory
	CreatedAs       template.TemplateCategory
	FormNotSendable bool
}

type PhoneFacts struct {
	Status businessphone.Status
}

type Run struct {
	Processed int
	Failures  map[int]int
}

type Snapshot struct {
	Pending         int
	Fresh           bool
	AccessAllowed   bool
	PlanInactive    bool
	TemplateMissing bool
	Unread          []string
	Template        *TemplateFacts
	Phone           *PhoneFacts
	ExitConfigured  *bool
	LastRun         Run
	CapRoom         *int
	BalanceShort    bool
	OptedOutPending int
}

type Finding struct {
	Code     Code    `json:"code"`
	Level    Level   `json:"level"`
	Observed float64 `json:"observed,omitempty"`
	Limit    float64 `json:"limit,omitempty"`
}

type Report struct {
	Findings []Finding
}

func Check(s Snapshot) Report {
	var r Report
	add := func(level Level, code Code, observed, limit float64) {
		r.Findings = append(r.Findings, Finding{Code: code, Level: level, Observed: observed, Limit: limit})
	}
	blocks(s, add)
	confirms(s, add)
	advice(s, add)
	return r
}

type adder func(level Level, code Code, observed, limit float64)

func blocks(s Snapshot, add adder) {
	if s.Pending <= 0 {
		add(Block, emptyCode(s.Fresh), 0, 0)
	}
	if !s.AccessAllowed {
		add(Block, CodeAccessRevoked, 0, 0)
	}
	if s.PlanInactive {
		add(Block, CodePlanInactive, 0, 0)
	}
	if s.TemplateMissing {
		add(Block, CodeTemplateMissing, 0, 0)
	}
	if len(s.Unread) > 0 {
		add(Block, CodeHealthUnreadable, float64(len(s.Unread)), 0)
	}
	if s.Template != nil && !s.Template.Ready {
		add(Block, CodeTemplateNotReady, 0, 0)
	}
	if s.Template != nil && s.Template.FormNotSendable {
		add(Block, CodeFormNotSendable, 0, 0)
	}
	if s.Phone != nil && s.Phone.Status.BlocksSending() {
		add(Block, CodePhoneUnavailable, 0, 0)
	}
	if s.CapRoom != nil && *s.CapRoom <= 0 {
		add(Block, CodeMonthlyCapFull, 0, 0)
	}
}

func confirms(s Snapshot, add adder) {
	if t := s.Template; t != nil {
		switch strings.ToUpper(t.Quality) {
		case "RED":
			add(Confirm, CodeTemplateRed, 0, 0)
		case "YELLOW":
			add(Confirm, CodeTemplateYellow, 0, 0)
		}
		if t.Flagged {
			add(Confirm, CodeTemplateFlagged, 0, 0)
		}
		if t.Category == template.TemplateCategoryMarketing && t.CreatedAs != "" && t.CreatedAs != template.TemplateCategoryMarketing {
			add(Confirm, CodeTemplateReclassified, 0, 0)
		}
	}
	if s.ExitConfigured != nil && !*s.ExitConfigured {
		add(Confirm, CodeNoExit, 0, 0)
	}
	if judged, failed := s.LastRun.templateFailures(); judged >= MinSample && share(failed, judged) >= FailureRateConfirm {
		add(Confirm, CodeLastRunFailures, share(failed, judged), FailureRateConfirm)
	}
	if n := s.LastRun.Failures[metaerror.SpamLimit]; n > 0 {
		add(Confirm, CodeLastRunSpamLimit, float64(n), 0)
	}
	if s.BalanceShort {
		add(Confirm, CodeBalanceShort, 0, 0)
	}
}

func advice(s Snapshot, add adder) {
	if s.LastRun.Processed >= MinSample {
		if rate := share(s.LastRun.Failures[metaerror.EngagementLimit], s.LastRun.Processed); rate >= EngagementRateAdvice {
			add(Advice, CodeLastRunEngagement, rate, EngagementRateAdvice)
		}
	}
	if n := s.LastRun.Failures[metaerror.Undeliverable]; n > 0 {
		add(Advice, CodeLastRunUndeliverable, float64(n), 0)
	}
	if s.OptedOutPending > 0 {
		add(Advice, CodeOptedOutPending, float64(s.OptedOutPending), 0)
	}
	if s.CapRoom != nil && *s.CapRoom > 0 && *s.CapRoom < s.Pending {
		add(Advice, CodeCapBelowPending, float64(*s.CapRoom), float64(s.Pending))
	}
}

func (r Run) templateFailures() (judged, failed int) {
	judged = r.Processed
	for code, n := range r.Failures {
		if metaerror.AboutContact(code) {
			judged -= n
			continue
		}
		failed += n
	}
	return judged, failed
}

func share(n, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(n) / float64(total)
}

func (r Report) Blocks() bool {
	return r.has(Block)
}

func (r Report) NeedsConfirmation() bool {
	return r.has(Confirm)
}

func (r Report) has(level Level) bool {
	for _, f := range r.Findings {
		if f.Level == level {
			return true
		}
	}
	return false
}

func (r Report) Fingerprint() string {
	var codes []string
	for _, f := range r.Findings {
		if f.Level == Confirm {
			codes = append(codes, string(f.Code))
		}
	}
	if len(codes) == 0 {
		return ""
	}
	sort.Strings(codes)
	sum := sha256.Sum256([]byte(strings.Join(codes, ",")))
	return hex.EncodeToString(sum[:8])
}

func (r Report) Confirmed(fingerprint string) bool {
	return !r.NeedsConfirmation() || fingerprint == r.Fingerprint()
}

func Blocked(code Code, observed float64) Report {
	return Report{Findings: []Finding{{Code: code, Level: Block, Observed: observed}}}
}

func (r Report) With(other Report) Report {
	return Report{Findings: append(append([]Finding{}, r.Findings...), other.Findings...)}
}

func emptyCode(fresh bool) Code {
	if fresh {
		return CodeAudienceEmpty
	}
	return CodeNothingPending
}
