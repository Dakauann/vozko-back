package carriercampaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
)

func TestRunApply(t *testing.T) {
	cases := []struct {
		name   string
		from   campaign.Status
		action campaign.Action
		want   campaign.Status
		err    error
	}{
		{"running pauses", campaign.StatusRunning, campaign.ActionPause, campaign.StatusPaused, nil},
		{"paused resumes", campaign.StatusPaused, campaign.ActionStart, campaign.StatusRunning, nil},
		{"running stops", campaign.StatusRunning, campaign.ActionStop, campaign.StatusStopped, nil},
		{"paused stops", campaign.StatusPaused, campaign.ActionStop, campaign.StatusStopped, nil},
		{"running cannot start", campaign.StatusRunning, campaign.ActionStart, campaign.StatusRunning, campaign.ErrAlreadyRunning},
		{"paused cannot pause", campaign.StatusPaused, campaign.ActionPause, campaign.StatusPaused, campaign.ErrNotRunning},
		{"stopped run is final", campaign.StatusStopped, campaign.ActionStart, campaign.StatusStopped, ErrRunFinished},
		{"completed run is final", campaign.StatusCompleted, campaign.ActionStop, campaign.StatusCompleted, ErrRunFinished},
		{"unknown status", campaign.Status("LOST"), campaign.ActionStop, campaign.Status("LOST"), campaign.ErrStatusInvalid},
		{"unknown action", campaign.StatusRunning, campaign.Action("SKIP"), campaign.StatusRunning, campaign.ErrActionInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Run{Status: tc.from}.Apply(tc.action)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got.Status != tc.want {
				t.Fatalf("status %s, want %s", got.Status, tc.want)
			}
		})
	}
}

func TestRunComplete(t *testing.T) {
	at := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	got, err := Run{Status: campaign.StatusRunning}.Complete(at)
	if err != nil || got.Status != campaign.StatusCompleted || got.CompletedAt == nil || !got.CompletedAt.Equal(at) {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, from := range []campaign.Status{campaign.StatusPaused, campaign.StatusStopped, campaign.StatusCompleted} {
		if _, err := (Run{Status: from}).Complete(at); err == nil {
			t.Fatalf("%s should not complete", from)
		}
	}
}

func TestRecipientStatusValid(t *testing.T) {
	for _, s := range []RecipientStatus{RecipientPending, RecipientSent, RecipientSkipped, RecipientFailed} {
		if !s.Valid() {
			t.Fatalf("%q should be valid", s)
		}
	}
	for _, s := range []RecipientStatus{"", "delivered", "PENDING"} {
		if s.Valid() {
			t.Fatalf("%q should be refused", s)
		}
	}
}

func TestRecipientSettle(t *testing.T) {
	at := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	pending := Recipient{ID: "r1", Status: RecipientPending}

	sent, err := pending.MarkSent("m1", at)
	if err != nil || sent.Status != RecipientSent || sent.MessageID != "m1" || sent.SentAt == nil || !sent.SentAt.Equal(at) {
		t.Fatalf("sent %+v, %v", sent, err)
	}
	if _, err := pending.MarkSent(" ", at); !errors.Is(err, ErrMessageIDRequired) {
		t.Fatalf("empty message id err = %v", err)
	}
	if _, err := sent.MarkSent("m2", at); !errors.Is(err, ErrRecipientSettled) {
		t.Fatalf("resend err = %v", err)
	}

	skipped, err := pending.Skip(SkipSuppressed)
	if err != nil || skipped.Status != RecipientSkipped || skipped.SkipReason != SkipSuppressed {
		t.Fatalf("skipped %+v, %v", skipped, err)
	}
	if _, err := pending.Skip("tired"); !errors.Is(err, ErrSkipReasonInvalid) {
		t.Fatalf("unknown reason err = %v", err)
	}
	if _, err := skipped.Skip(SkipBalance); !errors.Is(err, ErrRecipientSettled) {
		t.Fatalf("double skip err = %v", err)
	}

	failed, err := pending.Fail("provider_refused")
	if err != nil || failed.Status != RecipientFailed || failed.ErrorCode != "provider_refused" {
		t.Fatalf("failed %+v, %v", failed, err)
	}
	if _, err := pending.Fail(""); !errors.Is(err, ErrErrorCodeRequired) {
		t.Fatalf("empty code err = %v", err)
	}
	if _, err := failed.Fail("again"); !errors.Is(err, ErrRecipientSettled) {
		t.Fatalf("double fail err = %v", err)
	}
}

func TestSkipReasonValues(t *testing.T) {
	want := map[SkipReason]string{
		SkipNoPhone:         "no_phone",
		SkipOptedOut:        "opted_out",
		SkipBlocked:         "blocked",
		SkipSuppressed:      "suppressed",
		SkipFrequencyCap:    "frequency_cap",
		SkipDuplicate:       "duplicate",
		SkipMissingVariable: "missing_variable",
		SkipSenderPaused:    "sender_paused",
		SkipBalance:         "balance",
	}
	if len(AllSkipReasons()) != len(want) {
		t.Fatalf("AllSkipReasons has %d entries", len(AllSkipReasons()))
	}
	for _, r := range AllSkipReasons() {
		if string(r) != want[r] || !r.Valid() {
			t.Fatalf("%q unexpected", r)
		}
	}
	if SkipReason("").Valid() || SkipReason("cooldown").Valid() {
		t.Fatal("unknown reasons must be refused")
	}
}

func TestSkipReasonSharedMapping(t *testing.T) {
	cases := []struct {
		shared campaign.SkipReason
		local  SkipReason
		ok     bool
	}{
		{campaign.SkipNoIdentity, SkipNoPhone, true},
		{campaign.SkipBlocked, SkipBlocked, true},
		{campaign.SkipOptedOut, SkipOptedOut, true},
		{campaign.SkipMissingVariable, SkipMissingVariable, true},
		{campaign.SkipCooldown, "", false},
		{campaign.SkipOverCap, "", false},
	}
	for _, tc := range cases {
		got, ok := SkipFromShared(tc.shared)
		if got != tc.local || ok != tc.ok {
			t.Errorf("%s -> %q %v", tc.shared, got, ok)
		}
		if tc.ok {
			back, ok := tc.local.Shared()
			if !ok || back != tc.shared {
				t.Errorf("%s back -> %q %v", tc.local, back, ok)
			}
		}
	}
	for _, r := range []SkipReason{SkipSuppressed, SkipFrequencyCap, SkipDuplicate, SkipSenderPaused, SkipBalance} {
		if _, ok := r.Shared(); ok {
			t.Errorf("%s has no shared counterpart", r)
		}
	}
}

func TestSkipFor(t *testing.T) {
	ok := campaign.LeadFacts{Found: true, HasIdentity: true}
	cases := []struct {
		name  string
		kind  Kind
		facts RecipientFacts
		want  SkipReason
	}{
		{"eligible", KindMarketing, RecipientFacts{Lead: ok}, ""},
		{"no lead", KindMarketing, RecipientFacts{}, SkipNoPhone},
		{"no phone", KindMarketing, RecipientFacts{Lead: campaign.LeadFacts{Found: true}}, SkipNoPhone},
		{"blocked before opted out", KindMarketing, RecipientFacts{Lead: campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: true, OptedOut: true}}, SkipBlocked},
		{"opted out", KindTransactional, RecipientFacts{Lead: campaign.LeadFacts{Found: true, HasIdentity: true, OptedOut: true}}, SkipOptedOut},
		{"suppressed", KindTransactional, RecipientFacts{Lead: ok, Suppressed: true, Duplicate: true}, SkipSuppressed},
		{"duplicate", KindMarketing, RecipientFacts{Lead: ok, Duplicate: true, MissingVariable: true}, SkipDuplicate},
		{"missing variable", KindMarketing, RecipientFacts{Lead: ok, MissingVariable: true, OverFrequencyCap: true}, SkipMissingVariable},
		{"marketing over the cap", KindMarketing, RecipientFacts{Lead: ok, OverFrequencyCap: true, SenderPaused: true}, SkipFrequencyCap},
		{"transactional ignores the cap", KindTransactional, RecipientFacts{Lead: ok, OverFrequencyCap: true}, ""},
		{"unknown kind keeps the cap", Kind("promo"), RecipientFacts{Lead: ok, OverFrequencyCap: true}, SkipFrequencyCap},
		{"sender paused", KindTransactional, RecipientFacts{Lead: ok, SenderPaused: true, BalanceShort: true}, SkipSenderPaused},
		{"balance", KindMarketing, RecipientFacts{Lead: ok, BalanceShort: true}, SkipBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SkipFor(tc.kind, tc.facts); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
