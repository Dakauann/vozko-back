package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/usecases/campaignguard"
	"vozko/usecases/campaignqueue"
)

func TestTheConsumerFailsAnIneligibleEntryBeforeUsingTheDailyBudget(t *testing.T) {
	cases := []struct {
		reason campaign.SkipReason
		status campaign.SendStatus
		code   int
	}{
		{campaign.SkipBlocked, campaign.SendStatusFailed, 920001},
		{campaign.SkipOptedOut, campaign.SendStatusFailed, 920002},
		{campaign.SkipNoIdentity, campaign.SendStatusFailed, 920003},
		{campaign.SkipCooldown, campaign.SendStatusNotEligiblePossibleSpam, 920004},
	}
	for _, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			h := newHarness(t)
			campID, entryID := h.seed(t)
			h.spam.reasons = map[string]campaign.SkipReason{"lead-1": tc.reason}

			if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDrop {
				t.Fatalf("outcome = %v, want Drop", got.Outcome)
			}
			entry := h.entries.get(entryID)
			if entry.Status != tc.status || entry.ErrorCode != tc.code || entry.ErrorMessage != string(tc.reason) {
				t.Fatalf("entry = (%s, %d, %q), want (%s, %d, %q)", entry.Status, entry.ErrorCode, entry.ErrorMessage, tc.status, tc.code, tc.reason)
			}
			if h.sender.count() != 0 {
				t.Fatal("an ineligible lead was sent to")
			}
			if used := NewSendBudget(h.shared).UsedToday("inst-1"); used != 0 {
				t.Fatalf("an ineligible lead used %d of the daily budget", used)
			}
			if len(h.spam.senders) != 1 || h.spam.senders[0] != "inst-1" {
				t.Fatalf("eligibility was asked for senders %v, want the campaign number", h.spam.senders)
			}
		})
	}
}

func TestTheConsumerHoldsTheEntryWhenEligibilityIsUnknown(t *testing.T) {
	for name, breakIt := range map[string]func(h *harness){
		"the check failed":     func(h *harness) { h.spam.err = errors.New("db down") },
		"no check is wired in": func(h *harness) { h.consumer.deps.Eligibility = nil },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			campID, entryID := h.seed(t)
			breakIt(h)
			got := h.run(campID, entryID)
			if got.Outcome != campaignqueue.OutcomeRetryLater || got.Delay <= 0 {
				t.Fatalf("outcome = %+v, want a delayed retry", got)
			}
			if h.sender.count() != 0 || h.entries.get(entryID).Status != campaign.SendStatusPending {
				t.Fatal("nothing may be sent or marked while eligibility is unknown")
			}
			if used := NewSendBudget(h.shared).UsedToday("inst-1"); used != 0 {
				t.Fatalf("an unchecked entry used %d of the daily budget", used)
			}
		})
	}
}

func TestCreateMarksEveryIneligibleEntryWithItsCode(t *testing.T) {
	uc, _, entries, _, spam := newCreateHarness(t)
	spam.reasons = map[string]campaign.SkipReason{"lead-1": campaign.SkipOptedOut}

	created, err := uc.Execute(context.Background(), draft(), uw.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	failed, _ := entries.ListByStatus(created.ID, campaign.SendStatusFailed, 10)
	if len(failed) != 1 || failed[0].LeadID != "lead-1" || failed[0].ErrorCode != 920002 || failed[0].ErrorMessage != "opted_out" {
		t.Fatalf("failed entries = %+v, want the opted out lead with code 920002", failed)
	}
	pending, _ := entries.ListByStatus(created.ID, campaign.SendStatusPending, 10)
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if len(spam.screens) != 1 || len(spam.screens[0]) != 2 || spam.senders[0] != "inst-1" {
		t.Fatalf("screens = %v senders = %v, want one batch of both leads from the campaign number", spam.screens, spam.senders)
	}
}

func TestCreateWritesNothingWhenEligibilityIsUnknown(t *testing.T) {
	uc, campaigns, entries, _, spam := newCreateHarness(t)
	spam.err = errors.New("db down")
	if _, err := uc.Execute(context.Background(), draft(), uw.Unrestricted()); err == nil {
		t.Fatal("an unreadable eligibility must refuse the campaign")
	}
	if campaigns.count() != 0 || entries.count() != 0 {
		t.Fatalf("campaigns %d entries %d, want nothing written", campaigns.count(), entries.count())
	}

	bare := NewCreateCampaignUseCase(campaigns, entries, newFakeLeadRepo(), &fakeGateway{instance: &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected}}, nil, fakeDepartments{id: "dept-1"})
	bare.(*createCampaignUseCase).SetAutomation(automationThatAllows{})
	if _, err := bare.Execute(context.Background(), draft(), uw.Unrestricted()); !errors.Is(err, campaignguard.ErrUnavailable) {
		t.Fatalf("no eligibility check: err = %v, want ErrUnavailable", err)
	}
	if campaigns.count() != 0 || entries.count() != 0 {
		t.Fatal("nothing may be written without the eligibility check")
	}
}

func (f *fakeCampaignRepo) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.campaigns)
}

func (f *fakeEntryRepo) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

func TestTheConsumerHoldsAnIneligibleEntryUntilItsSkipIsWritten(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.spam.reasons = map[string]campaign.SkipReason{"lead-1": campaign.SkipOptedOut}
	h.entries.updateErr = errors.New("db down")

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeRetryLater || got.Delay <= 0 {
		t.Fatalf("outcome = %+v, want the entry held until the skip is stored", got)
	}
	if h.sender.count() != 0 || NewSendBudget(h.shared).UsedToday("inst-1") != 0 {
		t.Fatal("nothing may be sent or budgeted while the skip is unrecorded")
	}
}

func TestTheConsumerWaitsWhileAnotherSendHoldsTheContact(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.spam.claimErr = campaignguard.ErrSendClaimed

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeRetryLater || got.Delay <= 0 {
		t.Fatalf("outcome = %+v, want a delayed retry", got)
	}
	if h.sender.count() != 0 || NewSendBudget(h.shared).UsedToday("inst-1") != 0 {
		t.Fatal("a contact claimed by another send must not be sent to or budgeted")
	}
	if status := h.entries.get(entryID).Status; status != campaign.SendStatusPending {
		t.Fatalf("entry status = %s, want pending for the retry", status)
	}
}

func TestTheConsumerKeepsTheClaimAfterASendAndFreesItOtherwise(t *testing.T) {
	sent := newHarness(t)
	campID, entryID := sent.seed(t)
	if got := sent.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", got.Outcome)
	}
	if sent.spam.claims != 1 || sent.spam.released != 0 {
		t.Fatalf("claims = %d released = %d, want the claim kept until the cooldown is on record", sent.spam.claims, sent.spam.released)
	}

	broke := newHarness(t)
	campID, entryID = broke.seed(t)
	broke.sender.err = errors.New("provider down")
	broke.run(campID, entryID)
	if broke.spam.claims != 1 || broke.spam.released != 1 {
		t.Fatalf("claims = %d released = %d, want a failed send to free the contact", broke.spam.claims, broke.spam.released)
	}
}

type noSends struct{}

func (noSends) Record(string, string, string) error                { return nil }
func (noSends) GetLastSendTime(string, string) (*time.Time, error) { return nil, nil }
func (noSends) GetLastSendTimesBatch([]string, string) (map[string]time.Time, error) {
	return nil, nil
}

type leadFactsBook map[string]campaign.LeadFacts

func (b leadFactsBook) LeadFacts(_ context.Context, _ string, leadIDs []string) (map[string]campaign.LeadFacts, error) {
	out := map[string]campaign.LeadFacts{}
	for _, id := range leadIDs {
		if f, ok := b[id]; ok {
			out[id] = f
		}
	}
	return out, nil
}

type spamDays int

func (d spamDays) SpamProtectionDays(context.Context, string) (int, error) { return int(d), nil }

func TestARealBlockedOrOptedOutLeadNeverUsesTheDailyBudget(t *testing.T) {
	opted := campaign.LeadFacts{Found: true, HasIdentity: true, OptedOut: true}
	blocked := campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: true}
	for name, facts := range map[string]campaign.LeadFacts{"opted out": opted, "blocked": blocked} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			campID, entryID := h.seed(t)
			spam, err := campaignguard.NewSpamGuard(spamDays(3), noSends{}, h.shared, nil)
			if err != nil {
				t.Fatal(err)
			}
			eligibility, err := campaignguard.NewEligibility(leadFactsBook{"lead-1": facts}, spam)
			if err != nil {
				t.Fatal(err)
			}
			h.consumer.deps.Eligibility = eligibility

			if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDrop {
				t.Fatalf("outcome = %v, want Drop", got.Outcome)
			}
			if h.sender.count() != 0 || NewSendBudget(h.shared).UsedToday("inst-1") != 0 {
				t.Fatal("an ineligible lead was sent to or used the daily budget")
			}
		})
	}
}
