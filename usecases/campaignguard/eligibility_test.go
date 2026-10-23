package campaignguard

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/selection"
)

type guardFixture struct {
	facts    *screeningBook
	leads    *leadBook
	days     *fixedDays
	sends    *sendLog
	eligible *Eligibility
}

func newGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	f := &guardFixture{
		leads: &leadBook{facts: map[string]campaign.LeadFacts{}},
		days:  &fixedDays{days: 3},
		sends: &sendLog{last: map[string]time.Time{}},
	}
	spam, err := NewSpamGuard(f.days, f.sends, newClaimBook(), clock)
	if err != nil {
		t.Fatal(err)
	}
	f.facts = &screeningBook{leads: f.leads, sends: f.sends, running: map[string]bool{}}
	f.eligible, err = NewEligibility(f.facts, spam)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNewEligibilityRefusesAMissingDependency(t *testing.T) {
	spam, _ := NewSpamGuard(&fixedDays{}, &sendLog{}, newClaimBook(), clock)
	if _, err := NewEligibility(nil, spam); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no lead facts: %v", err)
	}
	if _, err := NewEligibility(&screeningBook{}, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no spam guard: %v", err)
	}
}

func TestScreenNamesEverySkippedLeadWithItsReason(t *testing.T) {
	f := newGuardFixture(t)
	f.leads.facts["ok"] = reachable()
	f.leads.facts["blocked"] = campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: true}
	f.leads.facts["quiet"] = campaign.LeadFacts{Found: true, HasIdentity: true, OptedOut: true}
	f.leads.facts["no-number"] = campaign.LeadFacts{Found: true}
	f.leads.facts["recent"] = reachable()
	f.leads.facts["unconsented"] = campaign.LeadFacts{Found: true, HasIdentity: true}
	f.sends.last["recent"] = guardNow.Add(-time.Hour)

	got, err := f.eligible.Screen(context.Background(), "ws-1", []string{"ok", "blocked", "quiet", "no-number", "recent", "unconsented", "gone"}, "bp-1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]campaign.SkipReason{
		"blocked":   campaign.SkipBlocked,
		"quiet":     campaign.SkipOptedOut,
		"no-number": campaign.SkipNoIdentity,
		"recent":    campaign.SkipCooldown,
		"gone":      campaign.SkipNoIdentity,
	}
	if len(got.Skipped) != len(want) || got.CooldownDays != 3 {
		t.Fatalf("Screen() = %+v, want %v with the 3 days in force", got, want)
	}
	for id, reason := range want {
		if got.Skipped[id] != reason {
			t.Fatalf("Screen()[%s] = %q, want %q", id, got.Skipped[id], reason)
		}
	}
}

func TestScreenNeverSkipsALeadForAMissingConsent(t *testing.T) {
	f := newGuardFixture(t)
	f.leads.facts["unconsented"] = campaign.LeadFacts{Found: true, HasIdentity: true}
	f.leads.facts["consented"] = reachable()
	got, err := f.eligible.Screen(context.Background(), "ws-1", []string{"unconsented", "consented"}, "bp-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skipped) != 0 {
		t.Fatalf("Screen() = %v, a missing consent is counted, never skipped", got)
	}
}

func TestScreenReadsLargeSelectionsInChunksAndTheCooldownOnce(t *testing.T) {
	f := newGuardFixture(t)
	ids := make([]string, 0, 11000)
	for i := 0; i < 11000; i++ {
		id := fmt.Sprintf("l-%05d", i)
		ids = append(ids, id)
		f.leads.facts[id] = reachable()
	}
	got, err := f.eligible.Screen(context.Background(), "ws-1", ids, "bp-1")
	if err != nil || len(got.Skipped) != 0 {
		t.Fatalf("Screen() = (%d skipped, %v), want none", len(got.Skipped), err)
	}
	if len(f.leads.batches) != 3 || len(f.sends.batches) != 3 {
		t.Fatalf("lead reads = %d, send reads = %d, want 3 chunks of at most %d", len(f.leads.batches), len(f.sends.batches), selection.Chunk)
	}
	if f.days.calls != 1 {
		t.Fatalf("cooldown policy reads = %d, want one", f.days.calls)
	}
}

func TestScreenFailsClosed(t *testing.T) {
	cases := map[string]func(f *guardFixture){
		"lead facts unreadable":      func(f *guardFixture) { f.leads.err = errBoom },
		"cooldown policy unreadable": func(f *guardFixture) { f.days.err = errBoom },
		"send log unreadable":        func(f *guardFixture) { f.sends.err = errBoom },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGuardFixture(t)
			f.leads.facts["l-1"] = reachable()
			breakIt(f)
			if got, err := f.eligible.Screen(context.Background(), "ws-1", []string{"l-1"}, "bp-1"); err == nil || got.Skipped != nil || got.CooldownDays != 0 {
				t.Fatalf("Screen() = (%+v, %v), want a refusal", got, err)
			}
			if reason, err := f.eligible.Check(context.Background(), "ws-1", "l-1", "bp-1"); err == nil || reason != "" {
				t.Fatalf("Check() = (%q, %v), want a refusal", reason, err)
			}
		})
	}
	f := newGuardFixture(t)
	if _, err := f.eligible.Screen(context.Background(), " ", []string{"l-1"}, "bp-1"); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("no workspace: %v", err)
	}
}

func TestCheckJudgesOneLead(t *testing.T) {
	f := newGuardFixture(t)
	f.leads.facts["ok"] = reachable()
	f.leads.facts["quiet"] = campaign.LeadFacts{Found: true, HasIdentity: true, OptedOut: true}
	if reason, err := f.eligible.Check(context.Background(), "ws-1", "ok", "bp-1"); err != nil || reason != "" {
		t.Fatalf("Check(ok) = (%q, %v)", reason, err)
	}
	if reason, err := f.eligible.Check(context.Background(), "ws-1", "quiet", "bp-1"); err != nil || reason != campaign.SkipOptedOut {
		t.Fatalf("Check(quiet) = (%q, %v)", reason, err)
	}
	if reason, err := f.eligible.Check(context.Background(), "ws-1", "", "bp-1"); err != nil || reason != campaign.SkipNoIdentity {
		t.Fatalf("Check(no lead) = (%q, %v), want no_identity", reason, err)
	}
}

func TestFactsLeaveRoomForTheCallerFacts(t *testing.T) {
	f := newGuardFixture(t)
	f.leads.facts["l-1"] = reachable()
	facts, err := f.eligible.Facts(context.Background(), "ws-1", []string{"l-1"}, "bp-1")
	if err != nil {
		t.Fatal(err)
	}
	got := facts["l-1"]
	if !got.Lead.Found || got.InCooldown {
		t.Fatalf("Facts() = %+v", got)
	}
	got.MissingVariable = true
	if ok, reason := campaign.Eligibility(got); ok || reason != campaign.SkipMissingVariable {
		t.Fatalf("a caller fact must be judged by the same rule, got (%v, %q)", ok, reason)
	}
}

func TestScreenWithRunningSkipsALeadPendingInARunningCampaignAfterItsOwnRefusals(t *testing.T) {
	f := newGuardFixture(t)
	for _, id := range []string{"running", "blocked-running", "recent-running", "free"} {
		f.leads.facts[id] = reachable()
	}
	f.leads.facts["blocked-running"] = campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: true}
	f.sends.last["recent-running"] = guardNow.Add(-time.Hour)
	f.facts.running = map[string]bool{"running": true, "blocked-running": true, "recent-running": true}

	got, err := f.eligible.ScreenWithRunning(context.Background(), "ws-1", []string{"running", "blocked-running", "recent-running", "free"}, "bp-1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]campaign.SkipReason{
		"running":         campaign.SkipAlreadyInRunningCampaign,
		"blocked-running": campaign.SkipBlocked,
		"recent-running":  campaign.SkipCooldown,
	}
	if len(got.Skipped) != len(want) {
		t.Fatalf("ScreenWithRunning() = %+v, want %v", got.Skipped, want)
	}
	for id, reason := range want {
		if got.Skipped[id] != reason {
			t.Fatalf("ScreenWithRunning()[%s] = %q, want %q", id, got.Skipped[id], reason)
		}
	}
	if len(f.facts.queries) != 1 || !f.facts.queries[0].Running || f.facts.queries[0].SenderID != "bp-1" {
		t.Fatalf("one read per chunk carries the sender and the running check, got %+v", f.facts.queries)
	}
}

func TestScreenNeverAsksForRunningCampaignsAndSkipsTheSendLogWithoutACooldown(t *testing.T) {
	f := newGuardFixture(t)
	f.leads.facts["l-1"] = reachable()
	f.facts.running = map[string]bool{"l-1": true}
	if got, err := f.eligible.Screen(context.Background(), "ws-1", []string{"l-1"}, "bp-1"); err != nil || len(got.Skipped) != 0 {
		t.Fatalf("Screen() = (%+v, %v), a running campaign is the caller's check", got, err)
	}
	f.days.days = 0
	if _, err := f.eligible.ScreenWithRunning(context.Background(), "ws-2", []string{"l-1"}, "bp-1"); err != nil {
		t.Fatal(err)
	}
	if q := f.facts.queries; len(q) != 2 || q[0].Running || q[1].SenderID != "" {
		t.Fatalf("queries = %+v, want no running check on Screen and no sender without a cooldown", q)
	}
}
