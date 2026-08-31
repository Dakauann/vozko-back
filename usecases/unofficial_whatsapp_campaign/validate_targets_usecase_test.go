package unofficial_whatsapp_campaign

import (
	"context"
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func newValidateHarness(t *testing.T) (uwc.ValidateTargetsUseCase, *fakeCampaignRepo, *fakeEntryRepo, *fakeGateway) {
	t.Helper()
	campaigns := newFakeCampaignRepo()
	entries := newFakeEntryRepo()
	gateway := &fakeGateway{instance: &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected,
	}}
	campaigns.put(&uwc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", InstanceID: "inst-1",
		Status: campaign.StatusStopped})

	for _, n := range []string{"5584999990001", "5584999990002"} {
		entries.put(&uwc.Entry{
			ID: "entry-" + n, CampaignID: "camp-1", LeadID: "lead-" + n,
			Number: n, Status: campaign.SendStatusPending,
		})
	}
	return NewValidateTargetsUseCase(campaigns, entries, gateway), campaigns, entries, gateway
}

// A dead number becomes a SKIP, never a failure: it is a fact about the list.
func TestValidateMarksDeadNumbersAsSkipped(t *testing.T) {
	uc, _, entries, gateway := newValidateHarness(t)
	gateway.checkResult = []uw.NumberCheck{
		{Query: "5584999990001", JID: "5584999990001@s.whatsapp.net", IsOnWhatsApp: true},
		{Query: "5584999990002", IsOnWhatsApp: false},
	}

	out, err := uc.Execute(context.Background(), "camp-1")
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if out.Checked != 2 || out.OnWhatsApp != 1 || out.Skipped != 1 {
		t.Fatalf("checked=%d live=%d skipped=%d, want 2/1/1", out.Checked, out.OnWhatsApp, out.Skipped)
	}

	skipped := entries.get("entry-5584999990002")
	if skipped.Status != campaign.SendStatusSkippedNotOnWhatsApp {
		t.Fatalf("status = %q, want SKIPPED_NOT_ON_WHATSAPP", skipped.Status)
	}
	// A dead number keeps no JID: leaving a stale one would let a later send
	// address an identity that does not exist.
	if skipped.JID != "" {
		t.Fatalf("a dead number kept a JID: %q", skipped.JID)
	}

	live := entries.get("entry-5584999990001")
	if live.Status != campaign.SendStatusPending {
		t.Fatalf("a live number was moved off PENDING: %q", live.Status)
	}
	if live.JID == "" {
		t.Fatal("a live number stored no JID, so the send path will re-check it")
	}
}

// Numbers the provider did not answer for are left alone: a missing answer is
// not a "no", and marking them dead would silently shrink the campaign.
func TestValidateLeavesUnansweredNumbersAlone(t *testing.T) {
	uc, _, entries, gateway := newValidateHarness(t)
	gateway.checkResult = []uw.NumberCheck{
		{Query: "5584999990001", JID: "x@s.whatsapp.net", IsOnWhatsApp: true},
	}

	out, err := uc.Execute(context.Background(), "camp-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Checked != 1 {
		t.Fatalf("checked = %d, want 1", out.Checked)
	}
	if got := entries.get("entry-5584999990002").Status; got != campaign.SendStatusPending {
		t.Fatalf("an unanswered number became %q", got)
	}
}

// A freshly-checked entry is not re-verified, so a second run costs nothing.
func TestValidateSkipsFreshlyCheckedEntries(t *testing.T) {
	uc, _, entries, gateway := newValidateHarness(t)
	now := time.Now().UTC()
	for _, id := range []string{"entry-5584999990001", "entry-5584999990002"} {
		e := entries.get(id)
		e.JID = "x@s.whatsapp.net"
		e.CheckedAt = &now
		entries.put(e)
	}

	out, err := uc.Execute(context.Background(), "camp-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Checked != 0 {
		t.Fatalf("checked = %d, want 0 for a freshly verified list", out.Checked)
	}
	if gateway.checkCalls != 0 {
		t.Fatalf("the provider was called %d times for a fresh list", gateway.checkCalls)
	}
}

// A dead session cannot answer the question. Refusing beats marking a whole
// list unreachable because OUR connection was down.
func TestValidateRefusesOnADeadSession(t *testing.T) {
	uc, _, _, gateway := newValidateHarness(t)
	gateway.instance.Status = uw.StatusDisconnected

	if _, err := uc.Execute(context.Background(), "camp-1"); err == nil {
		t.Fatal("validation ran against a disconnected number")
	}
}

// A provider failure keeps whatever progress was made: a second run resumes,
// because a checked entry is skipped by NeedsNumberCheck.
func TestValidateKeepsPartialProgressOnFailure(t *testing.T) {
	uc, _, _, gateway := newValidateHarness(t)
	gateway.checkErr = errBoom

	out, err := uc.Execute(context.Background(), "camp-1")
	if err == nil {
		t.Fatal("the provider failure did not surface")
	}
	if out == nil {
		t.Fatal("no partial result was returned, so the caller cannot report progress")
	}
}
