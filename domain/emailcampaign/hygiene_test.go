package emailcampaign

import (
	"slices"
	"testing"
	"time"

	"vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
)

func TestHygieneSkipMapsEveryCampaignBlockingVerdict(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	longAgo := now.AddDate(-1, 0, 0)
	cases := []struct {
		name     string
		facts    emailhygiene.Facts
		standing emailhygiene.Standing
		want     SkipReason
	}{
		{name: "a clean address is sent", standing: emailhygiene.Standing{Domain: emailhygiene.DomainAccepts}, want: SkipNone},
		{name: "a typo is a warning, not a skip", facts: emailhygiene.Facts{Suggestion: "gmail.com"}, standing: emailhygiene.Standing{Domain: emailhygiene.DomainAccepts}, want: SkipNone},
		{name: "a role address is a warning, not a skip", facts: emailhygiene.Facts{Role: true}, standing: emailhygiene.Standing{Domain: emailhygiene.DomainAccepts}, want: SkipNone},
		{name: "a workspace suppression keeps the suppressed reason", standing: emailhygiene.Standing{Suppression: es.SuppressSoftBounce, Domain: emailhygiene.DomainAccepts}, want: SkipSuppressed},
		{name: "a hard bounce in another workspace skips here", standing: emailhygiene.Standing{BadAddress: true, Domain: emailhygiene.DomainAccepts}, want: SkipBadAddress},
		{name: "a domain without mail is skipped", standing: emailhygiene.Standing{Domain: emailhygiene.DomainNoMail}, want: SkipNoMailDomain},
		{name: "a disposable address is skipped", facts: emailhygiene.Facts{Disposable: true}, standing: emailhygiene.Standing{Domain: emailhygiene.DomainAccepts}, want: SkipDisposable},
		{name: "an unknown domain is skipped, never treated as valid", standing: emailhygiene.Standing{Domain: emailhygiene.DomainUnknown}, want: SkipDomainUnknown},
		{name: "an unchecked domain waits for its check", standing: emailhygiene.Standing{Domain: emailhygiene.DomainPending}, want: SkipDomainChecking},
		{name: "a sunset contact is skipped", standing: emailhygiene.Standing{Domain: emailhygiene.DomainAccepts, Activity: emailhygiene.Activity{Known: true, SunsetSince: &longAgo, CampaignsSinceEngaged: 6}}, want: SkipInactive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HygieneSkip(tc.facts, tc.standing, now); got != tc.want {
				t.Fatalf("HygieneSkip = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewSkipReasonsAreKnownAndKeepAddressesUnique(t *testing.T) {
	for _, r := range []SkipReason{SkipBadAddress, SkipNoMailDomain, SkipDomainUnknown, SkipDomainChecking, SkipDisposable, SkipInactive} {
		if !r.Valid() {
			t.Fatalf("%s must be a known skip reason", r)
		}
		if !r.KeepsAddressUnique() {
			t.Fatalf("%s skips a normalized address, which stays unique per campaign", r)
		}
		if len(r) > 24 {
			t.Fatalf("%s does not fit the skip_reason column", r)
		}
	}
}

func TestPlanTestBatchGatesNewListsOnly(t *testing.T) {
	cases := []struct {
		name     string
		sendable int
		fresh    int
		want     int
	}{
		{name: "a known list is not gated", sendable: 10000, fresh: 1999, want: 0},
		{name: "twenty percent never mailed gates the campaign", sendable: 10000, fresh: 2000, want: 1000},
		{name: "a first campaign is gated", sendable: 5000, fresh: 5000, want: 500},
		{name: "a small new list still tests two hundred", sendable: 1200, fresh: 1200, want: 200},
		{name: "a huge new list tests one billing batch", sendable: 1000000, fresh: 1000000, want: 1000},
		{name: "two hundred recipients are the test themselves", sendable: 200, fresh: 200, want: 0},
		{name: "two hundred and one recipients are gated", sendable: 201, fresh: 201, want: 200},
		{name: "nothing to send is never gated", sendable: 0, fresh: 0, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlanTestBatch(tc.sendable, tc.fresh); got != tc.want {
				t.Fatalf("PlanTestBatch(%d, %d) = %d, want %d", tc.sendable, tc.fresh, got, tc.want)
			}
		})
	}
}

func TestDecideTestBatch(t *testing.T) {
	drained := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		outcome TestBatchOutcome
		now     time.Time
		want    GateDecision
	}{
		{name: "not drained yet waits", outcome: TestBatchOutcome{Sent: 500, Delivered: 500}, now: drained, want: GateWait},
		{name: "bounces above three percent fail at once", outcome: TestBatchOutcome{Sent: 1000, HardBounces: 31, Delivered: 100, Drained: true}, now: drained, want: GateFailBounces},
		{name: "exactly three percent of bounces is not above the line", outcome: TestBatchOutcome{Sent: 1000, HardBounces: 30, Delivered: 970, Drained: true}, now: drained.Add(11 * time.Minute), want: GatePass},
		{name: "complaints above a tenth of a percent fail", outcome: TestBatchOutcome{Sent: 1000, Complaints: 2, Delivered: 998, Drained: true}, now: drained.Add(time.Minute), want: GateFailComplaints},
		{name: "exactly a tenth of a percent of complaints is not above the line", outcome: TestBatchOutcome{Sent: 1000, Complaints: 1, Delivered: 999, Drained: true}, now: drained.Add(11 * time.Minute), want: GatePass},
		{name: "a clean batch still waits ten minutes for late bounces", outcome: TestBatchOutcome{Sent: 1000, Delivered: 1000, Drained: true}, now: drained.Add(9 * time.Minute), want: GateWait},
		{name: "a clean batch with most outcomes known passes after ten minutes", outcome: TestBatchOutcome{Sent: 1000, Delivered: 950, Drained: true}, now: drained.Add(10 * time.Minute), want: GatePass},
		{name: "too few outcomes known keeps waiting", outcome: TestBatchOutcome{Sent: 1000, Delivered: 940, Drained: true}, now: drained.Add(30 * time.Minute), want: GateWait},
		{name: "after an hour the known outcomes decide", outcome: TestBatchOutcome{Sent: 1000, Delivered: 600, Drained: true}, now: drained.Add(time.Hour), want: GatePass},
		{name: "a batch that sent nothing passes after the wait", outcome: TestBatchOutcome{Drained: true}, now: drained.Add(time.Hour), want: GatePass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.outcome.Drained {
				tc.outcome.DrainedAt = drained
			}
			if got := DecideTestBatch(tc.outcome, tc.now); got != tc.want {
				t.Fatalf("DecideTestBatch = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAFailedTestBatchCanOnlyBeStopped(t *testing.T) {
	for _, hold := range []HoldReason{HoldTestBatchBounces, HoldTestBatchComplaints} {
		c := Campaign{Status: StatusHeld, HoldReason: hold}
		actions := c.Actions()
		if slices.Contains(actions, CommandResume) || slices.Contains(actions, CommandPause) {
			t.Fatalf("%s actions = %v, want only stop", hold, actions)
		}
		if !slices.Contains(actions, CommandStop) {
			t.Fatalf("%s actions = %v, a held campaign can always be stopped", hold, actions)
		}
		if err := c.Resumable(); err != ErrTestBatchFailed {
			t.Fatalf("Resumable = %v, want ErrTestBatchFailed", err)
		}
	}
	held := Campaign{Status: StatusHeld, HoldReason: HoldBalanceShort}
	if err := held.Resumable(); err != nil {
		t.Fatalf("a balance hold can be resumed, got %v", err)
	}
	if !slices.Equal(held.Actions(), AllowedActions(StatusHeld)) {
		t.Fatalf("other holds keep the status actions")
	}
}

func TestDomainsCheckingBlocksTheLaunch(t *testing.T) {
	in := readyInput()
	in.Audience.Skipped = map[SkipReason]int{SkipDomainChecking: 3}
	report := LaunchCheck(in)
	var found *Issue
	for i := range report.Blocking {
		if report.Blocking[i].Code == IssueDomainsChecking {
			found = &report.Blocking[i]
		}
	}
	if found == nil || found.Addresses != 3 {
		t.Fatalf("blocking = %+v, want domains_checking with 3 addresses", report.Blocking)
	}
	if !slices.Contains(AllIssueCodes(), IssueDomainsChecking) {
		t.Fatal("the new issue code must be listed")
	}
}

func TestTestBatchHoldsAreKnownHoldReasons(t *testing.T) {
	for _, h := range []HoldReason{HoldTestBatchBounces, HoldTestBatchComplaints} {
		if !h.FailedTestBatch() {
			t.Fatalf("%s is a failed test batch", h)
		}
	}
	if HoldBalanceShort.FailedTestBatch() {
		t.Fatal("a balance hold is not a failed test batch")
	}
	if GateFailBounces.Hold() != HoldTestBatchBounces || GateFailComplaints.Hold() != HoldTestBatchComplaints || GatePass.Hold() != HoldNone {
		t.Fatal("each failing decision holds with its own reason")
	}
}

func TestSkipForSuppressionKeepsTheSharedReasons(t *testing.T) {
	cases := map[es.SuppressReason]SkipReason{
		es.SuppressBadAddress: SkipBadAddress, es.SuppressNoMailDomain: SkipNoMailDomain,
		es.SuppressHardBounce: SkipSuppressed, es.SuppressComplaint: SkipSuppressed, es.SuppressSoftBounce: SkipSuppressed, "": SkipSuppressed,
	}
	for reason, want := range cases {
		if got := SkipForSuppression(reason); got != want {
			t.Fatalf("SkipForSuppression(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestDomainsCheckingClearsOnItsOwnSoItNeverBlocksALaterStart(t *testing.T) {
	checking := LaunchReport{Blocking: []Issue{{Code: IssueDomainsChecking, Addresses: 2}}}
	if checking.Ready() || !checking.ReadyLater() || !checking.OnlyWaiting() {
		t.Fatal("unchecked domains block a start now but not a schedule")
	}
	mixed := LaunchReport{Blocking: []Issue{{Code: IssueDomainsChecking}, {Code: IssueBalanceInsufficient}}}
	if mixed.ReadyLater() || mixed.OnlyWaiting() {
		t.Fatal("any other issue still blocks")
	}
	if (LaunchReport{}).OnlyWaiting() {
		t.Fatal("a ready report is not waiting")
	}
}
