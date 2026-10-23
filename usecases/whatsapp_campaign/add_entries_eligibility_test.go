package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/usecases/campaignguard"
)

type recordedEntries struct {
	*mockEntryRepo
	created []wce.WhatsAppCampaignEntry
}

func (r *recordedEntries) Create(e *wce.WhatsAppCampaignEntry) error {
	r.created = append(r.created, *e)
	return nil
}

func addEntriesFixture(t *testing.T, screener campaignguard.Screener) (*addEntriesUseCase, *recordedEntries) {
	t.Helper()
	campaigns := newMockCampaignRepo()
	stoppedSelectionSend(campaigns, "")
	entries := &recordedEntries{mockEntryRepo: newMockEntryRepo()}
	uc := NewAddEntriesUseCase(campaigns, entries, &qsLeadRepo{}).(*addEntriesUseCase)
	uc.SetAutomation(permissiveAutomation(t))
	if screener != nil {
		uc.SetEligibility(screener)
	}
	return uc, entries
}

func TestAddedEntriesGoThroughTheSameEligibilityScreenAsTheCreate(t *testing.T) {
	screener := &fakeScreener{skip: map[string]campaign.SkipReason{"lead-558499991001": campaign.SkipBlocked}}
	uc, entries := addEntriesFixture(t, screener)
	out, err := uc.Execute(wc.AddEntriesInput{CampaignID: "camp-1", PhoneNumbers: validPhones(2)})
	if err != nil {
		t.Fatal(err)
	}
	if out.AddedCount != 2 || len(entries.created) != 2 {
		t.Fatalf("added %d, created %+v", out.AddedCount, entries.created)
	}
	pending, blocked := entries.created[0], entries.created[1]
	if pending.Status != wce.SendStatusPending || pending.ErrorCode != 0 {
		t.Fatalf("an eligible lead stays pending: %+v", pending)
	}
	if blocked.Status != campaign.SendStatusFailed || blocked.ErrorCode != campaign.SkipBlocked.FailureCode() || blocked.ErrorMessage != string(campaign.SkipBlocked) {
		t.Fatalf("a blocked lead is added already skipped: %+v", blocked)
	}
	if out.Entries[1].Status != string(campaign.SendStatusFailed) {
		t.Fatalf("the answer shows the skip: %+v", out.Entries[1])
	}
	if len(screener.calls) != 1 || screener.calls[0].senderID != "phone-1" || len(screener.calls[0].leadIDs) != 2 {
		t.Fatalf("screened %+v, want one screen of both leads with the campaign number", screener.calls)
	}
}

func TestAddEntriesFailsClosedWithoutTheEligibilityScreen(t *testing.T) {
	uc, entries := addEntriesFixture(t, nil)
	if _, err := uc.Execute(wc.AddEntriesInput{CampaignID: "camp-1", PhoneNumbers: validPhones(1)}); !errors.Is(err, campaignguard.ErrUnavailable) {
		t.Fatalf("Execute = %v, want the screen to be required", err)
	}
	if len(entries.created) != 0 {
		t.Fatal("an unscreened entry was written")
	}
	screener := &fakeScreener{err: errors.New("redis down")}
	broken, written := addEntriesFixture(t, screener)
	if _, err := broken.Execute(wc.AddEntriesInput{CampaignID: "camp-1", PhoneNumbers: validPhones(1)}); err == nil || len(written.created) != 0 {
		t.Fatalf("a failing screen wrote %+v, err %v", written.created, err)
	}
}
