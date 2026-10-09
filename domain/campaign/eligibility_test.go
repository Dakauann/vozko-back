package campaign

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/lead"
)

func sendable() EligibilityFacts {
	return EligibilityFacts{Lead: LeadFacts{Found: true, HasIdentity: true, HasConsent: true}}
}

func with(change func(*EligibilityFacts)) EligibilityFacts {
	f := sendable()
	change(&f)
	return f
}

func TestEligibility(t *testing.T) {
	cases := []struct {
		name   string
		facts  EligibilityFacts
		ok     bool
		reason SkipReason
	}{
		{name: "a reachable lead with consent", facts: sendable(), ok: true},
		{name: "a lead that no longer exists", facts: with(func(f *EligibilityFacts) { f.Lead.Found = false }), reason: SkipNoIdentity},
		{name: "a lead without a WhatsApp number", facts: with(func(f *EligibilityFacts) { f.Lead.HasIdentity = false }), reason: SkipNoIdentity},
		{name: "a blocked lead", facts: with(func(f *EligibilityFacts) { f.Lead.Blocked = true }), reason: SkipBlocked},
		{name: "an opted out lead", facts: with(func(f *EligibilityFacts) { f.Lead.OptedOut = true }), reason: SkipOptedOut},
		{name: "an opted out lead with consent left behind is still skipped", facts: with(func(f *EligibilityFacts) {
			f.Lead.OptedOut, f.Lead.HasConsent = true, true
		}), reason: SkipOptedOut},
		{name: "no consent is only counted, never skipped", facts: with(func(f *EligibilityFacts) { f.Lead.HasConsent = false }), ok: true},
		{name: "a lead messaged too recently", facts: with(func(f *EligibilityFacts) { f.InCooldown = true }), reason: SkipCooldown},
		{name: "a lead already in a running campaign", facts: with(func(f *EligibilityFacts) { f.InRunningCampaign = true }), reason: SkipAlreadyInRunningCampaign},
		{name: "a lead whose required variable resolved empty", facts: with(func(f *EligibilityFacts) { f.MissingVariable = true }), reason: SkipMissingVariable},
		{name: "a lead past the remaining cap", facts: with(func(f *EligibilityFacts) { f.OverCap = true }), reason: SkipOverCap},
		{name: "an open window is only counted", facts: with(func(f *EligibilityFacts) { f.WindowOpen = true }), ok: true},
		{name: "the missing identity wins over everything else", facts: EligibilityFacts{Lead: LeadFacts{Found: true, Blocked: true, OptedOut: true}, InCooldown: true}, reason: SkipNoIdentity},
		{name: "blocked wins over opted out", facts: with(func(f *EligibilityFacts) { f.Lead.Blocked, f.Lead.OptedOut = true, true }), reason: SkipBlocked},
		{name: "opted out wins over the cooldown", facts: with(func(f *EligibilityFacts) { f.Lead.OptedOut, f.InCooldown = true, true }), reason: SkipOptedOut},
		{name: "a lead without consent still cools down", facts: with(func(f *EligibilityFacts) {
			f.Lead.HasConsent, f.InCooldown = false, true
		}), reason: SkipCooldown},
		{name: "the cooldown wins over the cap", facts: with(func(f *EligibilityFacts) { f.InCooldown, f.OverCap = true, true }), reason: SkipCooldown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := Eligibility(tc.facts)
			if ok != tc.ok || reason != tc.reason {
				t.Fatalf("Eligibility() = (%v, %q), want (%v, %q)", ok, reason, tc.ok, tc.reason)
			}
		})
	}
}

func TestCountedReasons(t *testing.T) {
	cases := []struct {
		name  string
		facts EligibilityFacts
		want  []CountedReason
	}{
		{name: "nothing to note", facts: sendable(), want: nil},
		{name: "an open window", facts: with(func(f *EligibilityFacts) { f.WindowOpen = true }), want: []CountedReason{CountedWindowOpen}},
		{name: "no consent recorded", facts: with(func(f *EligibilityFacts) { f.Lead.HasConsent = false }), want: []CountedReason{CountedNoConsentRecorded}},
		{name: "both notes", facts: with(func(f *EligibilityFacts) { f.WindowOpen, f.Lead.HasConsent = true, false }), want: []CountedReason{CountedWindowOpen, CountedNoConsentRecorded}},
		{name: "a skipped lead is never counted", facts: with(func(f *EligibilityFacts) { f.WindowOpen, f.Lead.Blocked = true, true }), want: nil},
		{name: "a lead without consent that cools down is skipped, not noted", facts: with(func(f *EligibilityFacts) {
			f.Lead.HasConsent, f.InCooldown = false, true
		}), want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Counted(tc.facts); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Counted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTallyCountsEligibleSkippedAndNoted(t *testing.T) {
	var tally Tally
	tally.Add(sendable())
	tally.Add(with(func(f *EligibilityFacts) { f.WindowOpen = true }))
	tally.Add(with(func(f *EligibilityFacts) { f.Lead.HasConsent = false }))
	tally.Add(with(func(f *EligibilityFacts) { f.Lead.Blocked = true }))
	tally.Add(with(func(f *EligibilityFacts) { f.Lead.Blocked = true }))
	if ok, reason := tally.Add(with(func(f *EligibilityFacts) { f.Lead.OptedOut = true })); ok || reason != SkipOptedOut {
		t.Fatalf("Add() = (%v, %q), want the opted out skip", ok, reason)
	}

	want := Tally{
		Eligible: 3,
		Skipped:  map[SkipReason]int{SkipBlocked: 2, SkipOptedOut: 1},
		Counted:  map[CountedReason]int{CountedWindowOpen: 1, CountedNoConsentRecorded: 1},
	}
	if !reflect.DeepEqual(tally, want) {
		t.Fatalf("tally = %+v, want %+v", tally, want)
	}
	if tally.SkippedTotal() != 3 {
		t.Fatalf("SkippedTotal() = %d, want 3", tally.SkippedTotal())
	}
}

func TestSkipReasonMarksTheEntry(t *testing.T) {
	cases := []struct {
		reason SkipReason
		status SendStatus
		code   int
	}{
		{SkipBlocked, SendStatusFailed, 920001},
		{SkipOptedOut, SendStatusFailed, 920002},
		{SkipNoIdentity, SendStatusFailed, 920003},
		{SkipCooldown, SendStatusNotEligiblePossibleSpam, 920004},
		{SkipMissingVariable, SendStatusFailed, 920006},
		{SkipAlreadyInRunningCampaign, SendStatusFailed, 920007},
		{SkipOverCap, SendStatusFailed, 920008},
	}
	seen := map[int]bool{}
	for _, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			if !tc.reason.Valid() {
				t.Fatalf("%q is not a known reason", tc.reason)
			}
			if got := tc.reason.EntryStatus(); got != tc.status {
				t.Fatalf("EntryStatus() = %q, want %q", got, tc.status)
			}
			if got := tc.reason.FailureCode(); got != tc.code {
				t.Fatalf("FailureCode() = %d, want %d", got, tc.code)
			}
			if seen[tc.code] {
				t.Fatalf("code %d is used twice", tc.code)
			}
			seen[tc.code] = true
			back, ok := SkipReasonOfFailure(tc.code)
			if !ok || back != tc.reason {
				t.Fatalf("SkipReasonOfFailure(%d) = (%q, %v), want %q", tc.code, back, ok, tc.reason)
			}
		})
	}
	if len(SkipReasons()) != len(cases) {
		t.Fatalf("SkipReasons() lists %d reasons, the table covers %d", len(SkipReasons()), len(cases))
	}
	if SkipReason("").Valid() || SkipReason("spam").Valid() {
		t.Fatal("an unknown reason must not be valid")
	}
	if _, ok := SkipReasonOfFailure(900009); ok {
		t.Fatal("a failure code that is not a skip must not map to a reason")
	}
	if SkipReason("no_consent").Valid() {
		t.Fatal("a missing consent is counted, never a skip reason")
	}
	if _, ok := SkipReasonOfFailure(920005); ok {
		t.Fatal("the retired no_consent code must not map to a reason")
	}
	if SkipReason("spam").EntryStatus() != SendStatusFailed || SkipReason("spam").FailureCode() != 0 {
		t.Fatal("an unknown reason must fail the entry without a skip code")
	}
}

func TestAnUnknownErrorHasNoSendCode(t *testing.T) {
	if got := ErrorCode(errors.New("other")); got != "" {
		t.Fatalf("ErrorCode(other) = %q, want empty", got)
	}
}

func TestLeadRefusalJudgesTheLeadAlone(t *testing.T) {
	cases := []struct {
		name  string
		facts LeadFacts
		want  SkipReason
	}{
		{name: "a reachable lead", facts: LeadFacts{Found: true, HasIdentity: true}},
		{name: "a reachable lead without consent", facts: LeadFacts{Found: true, HasIdentity: true, HasConsent: false}},
		{name: "a lead that no longer exists", facts: LeadFacts{}, want: SkipNoIdentity},
		{name: "a lead without a number", facts: LeadFacts{Found: true}, want: SkipNoIdentity},
		{name: "a blocked lead", facts: LeadFacts{Found: true, HasIdentity: true, Blocked: true, OptedOut: true}, want: SkipBlocked},
		{name: "an opted out lead", facts: LeadFacts{Found: true, HasIdentity: true, OptedOut: true}, want: SkipOptedOut},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LeadRefusal(tc.facts); got != tc.want {
				t.Fatalf("LeadRefusal() = %q, want %q", got, tc.want)
			}
			if tc.want == "" {
				return
			}
			if ok, reason := Eligibility(EligibilityFacts{Lead: tc.facts}); ok || reason != tc.want {
				t.Fatalf("Eligibility() = (%v, %q), want the same refusal %q", ok, reason, tc.want)
			}
		})
	}
}

func TestLeadFactsOfReadsTheLeadRecord(t *testing.T) {
	opted := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		lead *lead.Lead
		want LeadFacts
	}{
		{name: "no lead", lead: nil, want: LeadFacts{}},
		{name: "a plain lead", lead: &lead.Lead{ID: "l-1", Number: "5511999999999"}, want: LeadFacts{Found: true, HasIdentity: true}},
		{name: "a lead without a number", lead: &lead.Lead{ID: "l-1", Number: " "}, want: LeadFacts{Found: true}},
		{name: "a blocked lead", lead: &lead.Lead{ID: "l-1", Number: "5511999999999", Blocked: true}, want: LeadFacts{Found: true, HasIdentity: true, Blocked: true}},
		{name: "an opted out lead", lead: &lead.Lead{ID: "l-1", Number: "5511999999999", OptedOutAt: &opted}, want: LeadFacts{Found: true, HasIdentity: true, OptedOut: true}},
		{name: "a lead with consent", lead: &lead.Lead{ID: "l-1", Number: "5511999999999", WhatsAppOptIn: &lead.Consent{GrantedAt: opted, Source: lead.ConsentManual}}, want: LeadFacts{Found: true, HasIdentity: true, HasConsent: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LeadFactsOf(tc.lead); got != tc.want {
				t.Fatalf("LeadFactsOf() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSkipReasonOutcomeIsWhatTheEntryRecords(t *testing.T) {
	for _, reason := range SkipReasons() {
		status, code, message := reason.Outcome()
		if status != reason.EntryStatus() || code != reason.FailureCode() || message != string(reason) {
			t.Fatalf("%s.Outcome() = (%s, %d, %q)", reason, status, code, message)
		}
	}
	if status, code, message := SkipCooldown.Outcome(); status != SendStatusNotEligiblePossibleSpam || code != 920004 || message != "cooldown" {
		t.Fatalf("cooldown outcome = (%s, %d, %q)", status, code, message)
	}
}
