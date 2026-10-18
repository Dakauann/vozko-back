package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func TestNumbersCannotBeAddedToAGroupCampaign(t *testing.T) {
	campaigns, entries, _ := stoppedGroupCampaign()
	uc := NewAddEntriesUseCase(campaigns, entries, newFakeLeadRepo())

	_, err := uc.Execute(context.Background(), uwc.AddEntriesInput{
		CampaignID: "camp-1", Numbers: []uwc.EntryInput{{Number: "5511981124410"}},
	})
	if !errors.Is(err, uwc.ErrCampaignAudienceMismatch) {
		t.Fatalf("Execute = %v, want ErrCampaignAudienceMismatch", err)
	}
}

func TestAGroupEntryCannotBeTurnedIntoANumber(t *testing.T) {
	campaigns, entries, _ := stoppedGroupCampaign()
	entry := uwc.NewGroupEntry("camp-1", "ws-1", "120363000000000001@g.us", "Ofertas VIP #1", time.Now())
	entry.ID = "entry-g"
	entries.put(&entry)
	number := "5511981124410"

	_, err := NewUpdateEntryUseCase(campaigns, entries, newFakeLeadRepo()).Execute(context.Background(), uwc.UpdateEntryInput{
		CampaignID: "camp-1", EntryID: "entry-g", Number: &number,
	})
	if !errors.Is(err, uwc.ErrCampaignAudienceMismatch) {
		t.Fatalf("Execute = %v, want ErrCampaignAudienceMismatch", err)
	}
	if entries.get("entry-g").Number != "" {
		t.Fatalf("a group entry was given a phone number")
	}
}
