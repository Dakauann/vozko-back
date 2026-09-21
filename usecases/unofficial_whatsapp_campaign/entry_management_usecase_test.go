package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func newEntryHarness(t *testing.T, status campaign.Status, bodies ...string) (*entryManagementUseCase, *fakeEntryRepo) {
	t.Helper()
	if len(bodies) == 0 {
		bodies = []string{"bom dia"}
	}
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{
		ID: "camp-1", WorkspaceID: "ws-1", InstanceID: "inst-1", Status: status,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: bodies},
	})
	entries := newFakeEntryRepo()
	return newEntryManagement(campaigns, entries, newFakeLeadRepo()), entries
}

func TestAddEntriesReportsWhatItRejected(t *testing.T) {
	uc, _ := newEntryHarness(t, campaign.StatusStopped)

	out, err := uc.add(context.Background(), uwc.AddEntriesInput{
		CampaignID: "camp-1",
		Numbers: []uwc.EntryInput{
			{Number: "5584999990001"},
			{Number: "+55 84 99999-0001"},
			{Number: "123"},
			{Number: "5584999990002"},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.AddedCount != 2 {
		t.Errorf("added = %d, want 2", out.AddedCount)
	}
	if out.DuplicatesSkipped != 1 {
		t.Errorf("duplicates = %d, want 1", out.DuplicatesSkipped)
	}
	if out.InvalidSkipped != 1 {
		t.Errorf("invalid = %d, want 1", out.InvalidSkipped)
	}
}

func TestAddEntriesRejectsRowsMissingVariables(t *testing.T) {
	uc, _ := newEntryHarness(t, campaign.StatusStopped, "oi {{1}} de {{2}}")

	out, err := uc.add(context.Background(), uwc.AddEntriesInput{
		CampaignID: "camp-1",
		Numbers: []uwc.EntryInput{
			{Number: "5584999990001", Variables: []string{"Ana"}},
			{Number: "5584999990002", Variables: []string{"Bia", "R$ 90"}},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.AddedCount != 1 || out.InvalidSkipped != 1 {
		t.Fatalf("added=%d invalid=%d, want 1/1", out.AddedCount, out.InvalidSkipped)
	}
}

func TestAddEntriesRefusedOnARunningCampaign(t *testing.T) {
	uc, _ := newEntryHarness(t, campaign.StatusRunning)

	_, err := uc.add(context.Background(), uwc.AddEntriesInput{
		CampaignID: "camp-1",
		Numbers:    []uwc.EntryInput{{Number: "5584999990001"}},
	}, false)
	if !errors.Is(err, uwc.ErrCampaignRunning) {
		t.Fatalf("err = %v, want ErrCampaignRunning", err)
	}
}

func TestQuickSendMayAddToARunningCampaign(t *testing.T) {
	uc, _ := newEntryHarness(t, campaign.StatusRunning)

	out, err := uc.add(context.Background(), uwc.AddEntriesInput{
		CampaignID: "camp-1",
		Numbers:    []uwc.EntryInput{{Number: "5584999990001"}},
	}, true)
	if err != nil {
		t.Fatalf("quick send was refused: %v", err)
	}
	if out.AddedCount != 1 {
		t.Fatalf("added = %d, want 1", out.AddedCount)
	}
}

func TestDeleteEntryChecksTheCampaignOwnsIt(t *testing.T) {
	uc, entries := newEntryHarness(t, campaign.StatusStopped)
	entries.put(&uwc.Entry{ID: "entry-1", CampaignID: "other-campaign", LeadID: "lead-1"})

	err := uc.deleteEntry(uwc.DeleteEntryInput{CampaignID: "camp-1", EntryID: "entry-1"})
	if !errors.Is(err, uwc.ErrEntryNotFound) {
		t.Fatalf("err = %v, want ErrEntryNotFound", err)
	}
}

func TestUpdateEntryRebindsTheLeadWhenTheNumberChanges(t *testing.T) {
	uc, entries := newEntryHarness(t, campaign.StatusStopped)
	entries.put(&uwc.Entry{
		ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-old",
		Number: "5584999990001", Status: campaign.SendStatusPending,
	})

	newNumber := "5584999990009"
	out, err := uc.updateEntry(context.Background(), uwc.UpdateEntryInput{
		CampaignID: "camp-1", EntryID: "entry-1", Number: &newNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.LeadID == "lead-old" {
		t.Fatal("the entry kept the previous number's lead")
	}
}

func TestUpdateEntryRejectsAnUnusableNumber(t *testing.T) {
	uc, entries := newEntryHarness(t, campaign.StatusStopped)
	entries.put(&uwc.Entry{ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-1", Number: "5584999990001"})

	bad := "123"
	if _, err := uc.updateEntry(context.Background(), uwc.UpdateEntryInput{
		CampaignID: "camp-1", EntryID: "entry-1", Number: &bad,
	}); !errors.Is(err, uwc.ErrCampaignTargetInvalid) {
		t.Fatalf("err = %v, want ErrCampaignTargetInvalid", err)
	}
}
