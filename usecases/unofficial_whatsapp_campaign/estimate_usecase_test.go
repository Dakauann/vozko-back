package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func paced(id string, status uw.Status, department string) *uw.Instance {
	return &uw.Instance{
		ID: id, WorkspaceID: "ws-1", Status: status, DepartmentID: strp(department), DisplayName: "Número " + id,
		SendDelayMinMS: 1000, SendDelayMaxMS: 1000,
	}
}

func estimateHarness() (uwc.EstimateCampaignUseCase, *fakeEntryRepo, *fakeGateway) {
	gateway := &fakeGateway{byID: map[string]*uw.Instance{
		"n1": paced("n1", uw.StatusConnected, "sales"),
		"n2": paced("n2", uw.StatusConnected, "sales"),
		"n3": paced("n3", uw.StatusConnected, "support"),
	}}
	directory := fakeSenderDirectory{
		instances: []*uw.Instance{gateway.byID["n1"], gateway.byID["n2"], gateway.byID["n3"]},
		seats: map[string][]*uw.Group{
			vipOne:  {groupSeat("n1", vipOne, "Ofertas VIP #1", false, false), groupSeat("n2", vipOne, "Ofertas VIP #1", false, false)},
			vipTwo:  {groupSeat("n1", vipTwo, "Ofertas VIP #2", false, false), groupSeat("n2", vipTwo, "Ofertas VIP #2", false, false)},
			notices: {groupSeat("n1", notices, "Avisos", true, false)},
			foreign: {groupSeat("n3", foreign, "Suporte interno", false, false)},
		},
	}
	entries := newFakeEntryRepo()
	return NewEstimateCampaignUseCase(entries, gateway, directory), entries, gateway
}

func pendingGroupEntry(entries *fakeEntryRepo, id, jid string) {
	entry := uwc.NewGroupEntry("camp-g", "ws-1", jid, jid, time.Now())
	entry.ID = id
	entries.put(&entry)
}

func TestRemainingOfAPersonCampaignIsPendingTimesTheNumberInterval(t *testing.T) {
	uc, entries, _ := estimateHarness()
	for _, id := range []string{"e1", "e2", "e3"} {
		entries.put(&uwc.Entry{ID: id, CampaignID: "camp-1", LeadID: "lead-" + id, Status: campaign.SendStatusPending})
	}
	entries.put(&uwc.Entry{ID: "e4", CampaignID: "camp-1", LeadID: "lead-4", Status: campaign.SendStatusSent})

	got, err := uc.Remaining(context.Background(), &uwc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", InstanceID: "n1", SendDelayMinMS: 4000, SendDelayMaxMS: 6000})

	if err != nil || got.Pending != 3 || got.Duration != 15*time.Second || got.Senders != 1 {
		t.Fatalf("Remaining = %+v, %v; want 3 pending at 5s each", got, err)
	}
}

func TestRemainingOfAGroupCampaignCountsTheNumbersThatSendInParallel(t *testing.T) {
	uc, entries, _ := estimateHarness()
	pendingGroupEntry(entries, "g1", vipOne)
	pendingGroupEntry(entries, "g2", vipTwo)
	pendingGroupEntry(entries, "g3", notices)

	got, err := uc.Remaining(context.Background(), &uwc.Campaign{
		ID: "camp-g", WorkspaceID: "ws-1", Audience: uwc.AudienceGroups, SenderInstanceIDs: []string{"n1", "n2"},
		SendDelayMinMS: 4000, SendDelayMaxMS: 6000,
	})

	if err != nil || got.Pending != 2 || got.Senders != 2 || got.Duration != 5*time.Second || got.Unsendable != 1 {
		t.Fatalf("Remaining = %+v, %v; want two groups sent side by side and the admins-only group left out", got, err)
	}
}

func TestDraftOfAGroupCampaignShowsWhoPostsInEachGroup(t *testing.T) {
	uc, _, _ := estimateHarness()

	got, err := uc.Draft(context.Background(), uwc.EstimateInput{
		WorkspaceID: "ws-1", Audience: uwc.AudienceGroups, GroupJIDs: []string{vipOne, notices, foreign},
		SendDelayMinMS: 4000, SendDelayMaxMS: 6000,
	}, uw.DepartmentScope{Restrict: true, DepartmentIDs: []string{"sales"}})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if len(got.Groups) != 3 {
		t.Fatalf("groups = %+v, want one plan per group", got.Groups)
	}
	if g := got.Groups[0]; g.Block != uwg.SenderReady || len(g.Senders) != 2 || g.Senders[0].Label != "Número n1" {
		t.Fatalf("vip group = %+v, want both sales numbers", g)
	}
	if got.Groups[1].Block != uwg.SenderCannotPost || got.Groups[2].Block != uwg.SenderNotInGroup {
		t.Fatalf("blocks = %q %q, want cannot post and not in group", got.Groups[1].Block, got.Groups[2].Block)
	}
	if got.Estimate.Pending != 1 || got.Estimate.Duration != 5*time.Second || got.Estimate.Unsendable != 2 {
		t.Fatalf("estimate = %+v, want only the postable group counted", got.Estimate)
	}
}

func TestDraftOfAPersonCampaignUsesTheNumberPacing(t *testing.T) {
	uc, _, _ := estimateHarness()

	got, err := uc.Draft(context.Background(), uwc.EstimateInput{WorkspaceID: "ws-1", Audience: uwc.AudienceLeads, InstanceID: "n1", Targets: 4}, uw.Unrestricted())

	if err != nil || got.Estimate.Pending != 4 || got.Estimate.Duration != 4*time.Second || len(got.Groups) != 0 {
		t.Fatalf("Draft = %+v, %v; want 4 sends at the number's 1s pacing", got, err)
	}
}

func TestDraftRefusesANumberOutsideTheScope(t *testing.T) {
	uc, _, _ := estimateHarness()

	_, err := uc.Draft(context.Background(), uwc.EstimateInput{WorkspaceID: "ws-1", Audience: uwc.AudienceLeads, InstanceID: "n3", Targets: 4},
		uw.DepartmentScope{Restrict: true, DepartmentIDs: []string{"sales"}})

	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("Draft = %v, want the out-of-scope number refused", err)
	}
}

func TestDraftOfAGroupCampaignFailsClosedWithoutTheDirectory(t *testing.T) {
	gateway := &fakeGateway{byID: map[string]*uw.Instance{}}
	uc := NewEstimateCampaignUseCase(newFakeEntryRepo(), gateway, nil)

	_, err := uc.Draft(context.Background(), uwc.EstimateInput{WorkspaceID: "ws-1", Audience: uwc.AudienceGroups, GroupJIDs: []string{vipOne}}, uw.Unrestricted())

	if !errors.Is(err, ErrGroupTargetsUnavailable) {
		t.Fatalf("Draft = %v, want ErrGroupTargetsUnavailable", err)
	}
}
