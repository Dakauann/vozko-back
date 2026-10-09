package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/usecases/campaignguard"
	"vozko/usecases/campaignqueue"
)

func seedRunningEntry(h *testHarness, leadID string) campaignqueue.Message {
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	h.campaignRepo.Create(&wc.Campaign{ID: "camp-1", Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1"})
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: "camp-1", LeadID: leadID, Status: wce.SendStatusPending}
	return campaignqueue.Message{CampaignID: "camp-1", EntryID: "entry-1", PhoneNumber: "5584999990001"}
}

func TestTheConsumerFailsAnIneligibleEntryBeforeAnyDebit(t *testing.T) {
	cases := []struct {
		reason campaign.SkipReason
		status wce.SendStatus
		code   int
	}{
		{campaign.SkipBlocked, wce.SendStatusFailed, 920001},
		{campaign.SkipOptedOut, wce.SendStatusFailed, 920002},
		{campaign.SkipNoIdentity, wce.SendStatusFailed, 920003},
		{campaign.SkipCooldown, wce.SendStatusNotEligiblePossibleSpam, 920004},
	}
	for _, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			h := newTestHarness()
			msg := seedRunningEntry(h, "lead-1")
			h.screener.skip["lead-1"] = tc.reason

			if got := h.consumer.handle(msg); got.Outcome != campaignqueue.OutcomeDrop {
				t.Fatalf("outcome = %v, want Drop", got.Outcome)
			}
			if n := len(h.consumeTempl.executeCalls); n != 0 {
				t.Fatalf("debited %d time(s) for an ineligible lead", n)
			}
			if n := h.waClient.sendCalls.Load(); n != 0 {
				t.Fatalf("sent %d message(s) to an ineligible lead", n)
			}
			entry := h.entryRepo.entries["entry-1"]
			if entry.Status != tc.status || entry.ErrorCode != tc.code || entry.ErrorMessage != string(tc.reason) {
				t.Fatalf("entry = (%s, %d, %q), want (%s, %d, %q)", entry.Status, entry.ErrorCode, entry.ErrorMessage, tc.status, tc.code, tc.reason)
			}
			if len(h.screener.calls) != 1 || h.screener.calls[0].senderID != "bp-1" || h.screener.calls[0].workspaceID != "ws-1" || h.screener.calls[0].leadIDs[0] != "lead-1" {
				t.Fatalf("eligibility calls = %+v", h.screener.calls)
			}
		})
	}
}

func TestTheConsumerSendsToAnEligibleLead(t *testing.T) {
	h := newTestHarness()
	msg := seedRunningEntry(h, "lead-1")
	if got := h.consumer.handle(msg); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", got.Outcome)
	}
	if len(h.consumeTempl.executeCalls) != 1 || h.waClient.sendCalls.Load() != 1 {
		t.Fatalf("debits = %d sends = %d, want one each", len(h.consumeTempl.executeCalls), h.waClient.sendCalls.Load())
	}
}

func TestTheConsumerHoldsTheEntryWhenEligibilityCannotBeRead(t *testing.T) {
	for name, breakIt := range map[string]func(h *testHarness){
		"the check failed":     func(h *testHarness) { h.screener.err = errors.New("db down") },
		"no check is wired in": func(h *testHarness) { h.consumer.Eligibility = nil },
	} {
		t.Run(name, func(t *testing.T) {
			h := newTestHarness()
			msg := seedRunningEntry(h, "lead-1")
			breakIt(h)
			got := h.consumer.handle(msg)
			if got.Outcome != campaignqueue.OutcomeRetryLater || got.Delay <= 0 {
				t.Fatalf("outcome = %+v, want a delayed retry", got)
			}
			if len(h.consumeTempl.executeCalls) != 0 || h.waClient.sendCalls.Load() != 0 {
				t.Fatal("nothing may be debited or sent while eligibility is unknown")
			}
			if status := h.entryRepo.entries["entry-1"].Status; status != wce.SendStatusPending {
				t.Fatalf("entry status = %s, want it left pending for the retry", status)
			}
		})
	}
}

func TestTheConsumerHoldsAnIneligibleEntryUntilItsSkipIsWritten(t *testing.T) {
	h := newTestHarness()
	msg := seedRunningEntry(h, "lead-1")
	h.screener.skip["lead-1"] = campaign.SkipOptedOut
	h.entryRepo.updateErr = errors.New("db down")

	got := h.consumer.handle(msg)
	if got.Outcome != campaignqueue.OutcomeRetryLater || got.Delay <= 0 {
		t.Fatalf("outcome = %+v, want the entry held until the skip is stored", got)
	}
	if len(h.consumeTempl.executeCalls) != 0 || h.waClient.sendCalls.Load() != 0 {
		t.Fatal("nothing may be debited or sent while the skip is unrecorded")
	}
}

func TestTheConsumerWaitsWhileAnotherSendHoldsTheContact(t *testing.T) {
	h := newTestHarness()
	msg := seedRunningEntry(h, "lead-1")
	h.screener.claimErr = campaignguard.ErrSendClaimed

	got := h.consumer.handle(msg)
	if got.Outcome != campaignqueue.OutcomeRetryLater || got.Delay <= 0 {
		t.Fatalf("outcome = %+v, want a delayed retry", got)
	}
	if len(h.consumeTempl.executeCalls) != 0 || h.waClient.sendCalls.Load() != 0 {
		t.Fatal("a contact claimed by another send must not be debited or sent to")
	}
	if status := h.entryRepo.entries["entry-1"].Status; status != wce.SendStatusPending {
		t.Fatalf("entry status = %s, want pending for the retry", status)
	}
}

func TestTheConsumerKeepsTheClaimAfterASendAndFreesItOtherwise(t *testing.T) {
	sent := newTestHarness()
	if got := sent.consumer.handle(seedRunningEntry(sent, "lead-1")); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", got.Outcome)
	}
	if sent.screener.claims != 1 || sent.screener.released != 0 {
		t.Fatalf("claims = %d released = %d, want the claim kept until the cooldown is on record", sent.screener.claims, sent.screener.released)
	}

	broke := newTestHarness()
	msg := seedRunningEntry(broke, "lead-1")
	broke.waClient.failSend = true
	broke.consumer.handle(msg)
	if broke.screener.claims != 1 || broke.screener.released != 1 {
		t.Fatalf("claims = %d released = %d, want a failed send to free the contact", broke.screener.claims, broke.screener.released)
	}

	poor := newTestHarness()
	msg = seedRunningEntry(poor, "lead-1")
	poor.cachedBalanceChecker.balanceMicros = 0
	poor.consumer.handle(msg)
	if poor.screener.released != poor.screener.claims {
		t.Fatalf("claims = %d released = %d, want an unsent entry to free the contact", poor.screener.claims, poor.screener.released)
	}
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

func TestARealBlockedOrOptedOutLeadIsNeverDebited(t *testing.T) {
	opted := campaign.LeadFacts{Found: true, HasIdentity: true, OptedOut: true}
	blocked := campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: true}
	for name, facts := range map[string]campaign.LeadFacts{"opted out": opted, "blocked": blocked} {
		t.Run(name, func(t *testing.T) {
			h := newTestHarness()
			msg := seedRunningEntry(h, "lead-1")
			spam, err := campaignguard.NewSpamGuard(spamDays(3), &mockLeadCampaignSendRepo{}, h.shared, nil)
			if err != nil {
				t.Fatal(err)
			}
			eligibility, err := campaignguard.NewEligibility(leadFactsBook{"lead-1": facts}, spam)
			if err != nil {
				t.Fatal(err)
			}
			h.consumer.Eligibility = eligibility

			if got := h.consumer.handle(msg); got.Outcome != campaignqueue.OutcomeDrop {
				t.Fatalf("outcome = %v, want Drop", got.Outcome)
			}
			if len(h.consumeTempl.executeCalls) != 0 || h.waClient.sendCalls.Load() != 0 {
				t.Fatal("an ineligible lead was debited or sent to")
			}
		})
	}
}
