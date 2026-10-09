package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func stoppedUnofficial(source string) (*fakeCampaignRepo, *fakeEntryRepo, *fakeGateway) {
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{
		ID: "camp-1", WorkspaceID: "ws-1", InstanceID: "inst-1", Name: "Matrículas", Status: campaign.StatusStopped, Source: source,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Olá {{1}}"}},
	})
	gateway := &fakeGateway{instance: &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected}}
	return campaigns, newFakeEntryRepo(), gateway
}

func edited(message string) *uwc.Campaign {
	return &uwc.Campaign{Name: "Matrículas", Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{message}}}
}

func TestAnUnofficialSelectionSendKeepsTheMessageItWasReviewedWith(t *testing.T) {
	cases := []struct {
		name  string
		input *uwc.Campaign
		err   error
	}{
		{name: "another message", input: edited("Oi {{1}}"), err: campaign.ErrSelectionSendLocked},
		{name: "the same message", input: edited("Olá {{1}}")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			campaigns, entries, gateway := stoppedUnofficial(campaign.SourceLeadSelection)
			_, err := NewUpdateCampaignUseCase(campaigns, entries, gateway).Execute(context.Background(), "camp-1", tc.input, uw.Unrestricted())
			if !errors.Is(err, tc.err) {
				t.Fatalf("Execute = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestUnofficialUpdateRunsTheSharedAutomationStep(t *testing.T) {
	campaigns, entries, gateway := stoppedUnofficial("")
	in := edited("Olá {{1}}")
	in.WorkflowID, in.EnableWorkflow = "wf-1", true
	uc := NewUpdateCampaignUseCase(campaigns, entries, gateway)
	if _, err := uc.Execute(context.Background(), "camp-1", in, uw.Unrestricted()); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Execute without the step = %v, want ErrAutomationUnavailable", err)
	}
	uc.(*updateCampaignUseCase).SetAutomation(automationThatRefuses{err: campaign.ErrWorkflowForbidden})
	if _, err := uc.Execute(context.Background(), "camp-1", in, uw.Unrestricted()); !errors.Is(err, campaign.ErrWorkflowForbidden) {
		t.Fatalf("Execute = %v, want ErrWorkflowForbidden", err)
	}
	if got, _ := campaigns.FindByID("camp-1"); got.WorkflowID != "" {
		t.Fatal("a refused workflow was saved")
	}
}

func TestEntriesCannotBeAddedToAnUnofficialSelectionSend(t *testing.T) {
	campaigns, entries, _ := stoppedUnofficial(campaign.SourceLeadSelection)
	_, err := NewAddEntriesUseCase(campaigns, entries, newFakeLeadRepo()).Execute(context.Background(), uwc.AddEntriesInput{
		CampaignID: "camp-1", Numbers: []uwc.EntryInput{{Number: "5584999990001", Variables: []string{"Ana"}}},
	})
	if !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("Execute = %v, want ErrSelectionSendLocked", err)
	}
	if entries.count() != 0 {
		t.Fatal("a refused entry was written")
	}
}

func TestUnofficialAddEntriesRunsTheSharedAutomationStep(t *testing.T) {
	campaigns, entries, _ := stoppedUnofficial("")
	c, _ := campaigns.FindByID("camp-1")
	c.WorkflowID = "wf-1"
	campaigns.put(c)
	uc := NewAddEntriesUseCase(campaigns, entries, newFakeLeadRepo())
	in := uwc.AddEntriesInput{CampaignID: "camp-1", Numbers: []uwc.EntryInput{{Number: "5584999990001", Variables: []string{"Ana"}}}}
	if _, err := uc.Execute(context.Background(), in); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Execute without the step = %v, want ErrAutomationUnavailable", err)
	}
	uc.(addEntriesAdapter).SetAutomation(automationThatRefuses{err: campaign.ErrWorkflowVarsMissing})
	if _, err := uc.Execute(context.Background(), in); !errors.Is(err, campaign.ErrWorkflowVarsMissing) {
		t.Fatalf("Execute = %v, want ErrWorkflowVarsMissing", err)
	}
}

func TestAnUnofficialQuickSendNeverStartsASelectionSend(t *testing.T) {
	campaigns, entries, _ := stoppedUnofficial(campaign.SourceLeadSelection)
	entries.put(&uwc.Entry{ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-1", Number: "5584999990001", Status: campaign.SendStatusPending})
	dispatch := &dispatchRecorder{}
	_, err := NewQuickSendUseCase(campaigns, entries, newFakeLeadRepo(), dispatch, nil).Execute(context.Background(), uwc.QuickSendInput{CampaignID: "camp-1"})
	if !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("Execute = %v, want ErrSelectionSendLocked", err)
	}
	if len(dispatch.inputs) != 0 {
		t.Fatalf("a refused quick send dispatched %+v", dispatch.inputs)
	}
}

func TestTheRecipientsOfAnUnofficialSelectionSendCannotBeEdited(t *testing.T) {
	number, name := "5584999990002", "Bia"
	cases := []struct {
		name   string
		source string
		input  uwc.UpdateEntryInput
		err    error
	}{
		{name: "another number", source: campaign.SourceLeadSelection, input: uwc.UpdateEntryInput{Number: &number}, err: campaign.ErrSelectionSendLocked},
		{name: "another name", source: campaign.SourceLeadSelection, input: uwc.UpdateEntryInput{Name: &name}, err: campaign.ErrSelectionSendLocked},
		{name: "other variables", source: campaign.SourceLeadSelection, input: uwc.UpdateEntryInput{Variables: []string{"Bia"}}, err: campaign.ErrSelectionSendLocked},
		{name: "other metadata", source: campaign.SourceLeadSelection, input: uwc.UpdateEntryInput{Metadata: map[string]interface{}{"k": "v"}}, err: campaign.ErrSelectionSendLocked},
		{name: "a plain campaign", input: uwc.UpdateEntryInput{Variables: []string{"Bia"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			campaigns, entries, _ := stoppedUnofficial(tc.source)
			entries.put(&uwc.Entry{ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-1", Number: "5584999990001", Variables: []string{"Ana"}, Status: campaign.SendStatusPending})
			tc.input.CampaignID, tc.input.EntryID = "camp-1", "entry-1"

			_, err := NewUpdateEntryUseCase(campaigns, entries, newFakeLeadRepo()).Execute(context.Background(), tc.input)

			if !errors.Is(err, tc.err) {
				t.Fatalf("Execute = %v, want %v", err, tc.err)
			}
			if got := entries.get("entry-1"); tc.err != nil && (got.Number != "5584999990001" || got.LeadID != "lead-1" || got.Variables[0] != "Ana") {
				t.Fatalf("a refused edit changed the entry: %+v", got)
			}
		})
	}
}

func TestAnUnofficialSelectionSendCannotBeReset(t *testing.T) {
	campaigns, entries, _ := stoppedUnofficial(campaign.SourceLeadSelection)
	c, _ := campaigns.FindByID("camp-1")
	c.ResetCode = "123456"
	campaigns.put(c)
	uc := NewResetCampaignUseCase(campaigns, entries)

	if _, err := uc.PrepareReset("camp-1"); !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("PrepareReset = %v, want ErrSelectionSendLocked", err)
	}
	if _, err := uc.ConfirmReset(uwc.ResetCampaignInput{CampaignID: "camp-1", ResetCode: "123456"}); !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("ConfirmReset = %v, want ErrSelectionSendLocked", err)
	}
	if got, _ := campaigns.FindByID("camp-1"); got.ResetCode != "123456" {
		t.Fatalf("a refused reset touched the campaign: code %q", got.ResetCode)
	}
}
