package campaign

import (
	"strings"

	"vozko/domain/lead"
)

type SkipReason string

const (
	SkipBlocked                  SkipReason = "blocked"
	SkipOptedOut                 SkipReason = "opted_out"
	SkipNoIdentity               SkipReason = "no_identity"
	SkipCooldown                 SkipReason = "cooldown"
	SkipMissingVariable          SkipReason = "missing_variable"
	SkipAlreadyInRunningCampaign SkipReason = "already_in_running_campaign"
	SkipOverCap                  SkipReason = "over_cap"
)

type CountedReason string

const (
	CountedWindowOpen        CountedReason = "window_open"
	CountedNoConsentRecorded CountedReason = "no_consent_recorded"
)

var skipFailureCodes = map[SkipReason]int{
	SkipBlocked:                  920001,
	SkipOptedOut:                 920002,
	SkipNoIdentity:               920003,
	SkipCooldown:                 920004,
	SkipMissingVariable:          920006,
	SkipAlreadyInRunningCampaign: 920007,
	SkipOverCap:                  920008,
}

func SkipReasons() []SkipReason {
	return []SkipReason{
		SkipNoIdentity, SkipBlocked, SkipOptedOut,
		SkipCooldown, SkipAlreadyInRunningCampaign, SkipMissingVariable, SkipOverCap,
	}
}

func (r SkipReason) Valid() bool {
	_, ok := skipFailureCodes[r]
	return ok
}

func (r SkipReason) FailureCode() int {
	return skipFailureCodes[r]
}

func (r SkipReason) EntryStatus() SendStatus {
	if r == SkipCooldown {
		return SendStatusNotEligiblePossibleSpam
	}
	return SendStatusFailed
}

func (r SkipReason) Outcome() (SendStatus, int, string) {
	return r.EntryStatus(), r.FailureCode(), string(r)
}

func SkipReasonOfFailure(code int) (SkipReason, bool) {
	for reason, known := range skipFailureCodes {
		if known == code {
			return reason, true
		}
	}
	return "", false
}

type LeadFacts struct {
	Found       bool
	HasIdentity bool
	Blocked     bool
	OptedOut    bool
	HasConsent  bool
}

type EligibilityFacts struct {
	Lead              LeadFacts
	InCooldown        bool
	InRunningCampaign bool
	MissingVariable   bool
	OverCap           bool
	WindowOpen        bool
}

func Eligibility(f EligibilityFacts) (bool, SkipReason) {
	reason := skipReasonOf(f)
	return reason == "", reason
}

func LeadRefusal(l LeadFacts) SkipReason {
	switch {
	case !l.Found || !l.HasIdentity:
		return SkipNoIdentity
	case l.Blocked:
		return SkipBlocked
	case l.OptedOut:
		return SkipOptedOut
	}
	return ""
}

func LeadFactsOf(l *lead.Lead) LeadFacts {
	if l == nil {
		return LeadFacts{}
	}
	return LeadFacts{
		Found:       true,
		HasIdentity: strings.TrimSpace(l.Number) != "",
		Blocked:     l.Blocked,
		OptedOut:    l.OptedOutAt != nil,
		HasConsent:  l.WhatsAppOptIn != nil,
	}
}

func skipReasonOf(f EligibilityFacts) SkipReason {
	if reason := LeadRefusal(f.Lead); reason != "" {
		return reason
	}
	if f.InCooldown {
		return SkipCooldown
	}
	return CampaignSkip(f.InRunningCampaign, f.MissingVariable, f.OverCap)
}

func FirstSkip(reasons ...SkipReason) SkipReason {
	for _, reason := range reasons {
		if reason != "" {
			return reason
		}
	}
	return ""
}

func CampaignSkip(inRunningCampaign, missingVariable, overCap bool) SkipReason {
	switch {
	case inRunningCampaign:
		return SkipAlreadyInRunningCampaign
	case missingVariable:
		return SkipMissingVariable
	case overCap:
		return SkipOverCap
	}
	return ""
}

func Counted(f EligibilityFacts) []CountedReason {
	if ok, _ := Eligibility(f); !ok {
		return nil
	}
	var counted []CountedReason
	if f.WindowOpen {
		counted = append(counted, CountedWindowOpen)
	}
	if !f.Lead.HasConsent {
		counted = append(counted, CountedNoConsentRecorded)
	}
	return counted
}

type Tally struct {
	Eligible int
	Skipped  map[SkipReason]int
	Counted  map[CountedReason]int
}

func (t *Tally) Add(f EligibilityFacts) (bool, SkipReason) {
	ok, reason := Eligibility(f)
	if !ok {
		if t.Skipped == nil {
			t.Skipped = map[SkipReason]int{}
		}
		t.Skipped[reason]++
		return false, reason
	}
	t.Eligible++
	for _, counted := range Counted(f) {
		if t.Counted == nil {
			t.Counted = map[CountedReason]int{}
		}
		t.Counted[counted]++
	}
	return true, ""
}

func (t Tally) SkippedTotal() int {
	total := 0
	for _, n := range t.Skipped {
		total += n
	}
	return total
}
