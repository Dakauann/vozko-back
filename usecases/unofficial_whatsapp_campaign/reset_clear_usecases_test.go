package unofficial_whatsapp_campaign

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type fakeWiper struct {
	wiped []string
	err   error
}

func (f *fakeWiper) DeleteByEntry(entryID string, _ shared.EntryType) error {
	if f.err != nil {
		return f.err
	}
	f.wiped = append(f.wiped, entryID)
	return nil
}

func seedForReset(status campaign.Status) (*fakeCampaignRepo, *fakeEntryRepo) {
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", Status: status})
	return campaigns, newFakeEntryRepo()
}

// The code is the only thing between a misclick and returning a whole campaign
// to pending, so a wrong one is refused and the right one works exactly once.
func TestResetRequiresTheIssuedCode(t *testing.T) {
	campaigns, entries := seedForReset(campaign.StatusStopped)
	uc := NewResetCampaignUseCase(campaigns, entries)

	prepared, err := uc.PrepareReset("camp-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.ResetCode) != campaign.ConfirmationCodeLength {
		t.Fatalf("code = %q, want %d digits", prepared.ResetCode, campaign.ConfirmationCodeLength)
	}

	if _, err := uc.ConfirmReset(uwc.ResetCampaignInput{
		CampaignID: "camp-1", ResetCode: "000000",
	}); !errors.Is(err, uwc.ErrCampaignResetCodeInvalid) {
		t.Fatalf("a wrong code was accepted: %v", err)
	}

	if _, err := uc.ConfirmReset(uwc.ResetCampaignInput{
		CampaignID: "camp-1", ResetCode: prepared.ResetCode,
	}); err != nil {
		t.Fatalf("the issued code was refused: %v", err)
	}

	// The code is consumed, so a replayed confirmation cannot reset twice.
	if _, err := uc.ConfirmReset(uwc.ResetCampaignInput{
		CampaignID: "camp-1", ResetCode: prepared.ResetCode,
	}); !errors.Is(err, uwc.ErrCampaignResetCodeInvalid) {
		t.Fatal("the code was reusable")
	}
}

// Resetting a live campaign would return entries to pending underneath a
// consumer still sending them, producing duplicates.
func TestResetRefusedWhileRunning(t *testing.T) {
	campaigns, entries := seedForReset(campaign.StatusRunning)
	uc := NewResetCampaignUseCase(campaigns, entries)

	if _, err := uc.PrepareReset("camp-1"); !errors.Is(err, uwc.ErrCampaignResetNotAllowed) {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := uc.ConfirmReset(uwc.ResetCampaignInput{
		CampaignID: "camp-1", ResetCode: "123456",
	}); !errors.Is(err, uwc.ErrCampaignResetNotAllowed) {
		t.Fatalf("confirm: %v", err)
	}
}

// A reset campaign is STOPPED, not left completed: it has work to do again.
func TestResetReturnsTheCampaignToStopped(t *testing.T) {
	campaigns, entries := seedForReset(campaign.StatusCompleted)
	uc := NewResetCampaignUseCase(campaigns, entries)

	prepared, _ := uc.PrepareReset("camp-1")
	if _, err := uc.ConfirmReset(uwc.ResetCampaignInput{
		CampaignID: "camp-1", ResetCode: prepared.ResetCode,
	}); err != nil {
		t.Fatal(err)
	}
	c, _ := campaigns.FindByID("camp-1")
	if c.Status != campaign.StatusStopped {
		t.Fatalf("status = %q, want STOPPED", c.Status)
	}
}

func TestClearHistoryRequiresTheIssuedCode(t *testing.T) {
	campaigns, entries := seedForReset(campaign.StatusStopped)
	wiper := &fakeWiper{}
	uc := NewClearHistoryUseCase(campaigns, entries, wiper)

	prepared, err := uc.PrepareClearHistory("camp-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.ConfirmClearHistory(uwc.ClearHistoryInput{
		CampaignID: "camp-1", ClearCode: "999999",
	}); !errors.Is(err, uwc.ErrCampaignClearCodeInvalid) {
		t.Fatalf("a wrong code was accepted: %v", err)
	}
	if _, err := uc.ConfirmClearHistory(uwc.ClearHistoryInput{
		CampaignID: "camp-1", ClearCode: prepared.ClearCode,
	}); err != nil {
		t.Fatalf("the issued code was refused: %v", err)
	}
}

func TestClearHistoryRefusedWhileRunning(t *testing.T) {
	campaigns, entries := seedForReset(campaign.StatusRunning)
	uc := NewClearHistoryUseCase(campaigns, entries, &fakeWiper{})

	if _, err := uc.PrepareClearHistory("camp-1"); !errors.Is(err, uwc.ErrCampaignClearNotAllowed) {
		t.Fatalf("err = %v, want ErrCampaignClearNotAllowed", err)
	}
}

// One conversation that cannot be wiped must not abort the rest: a partially
// cleared campaign is recoverable by running it again, while stopping halfway
// leaves the operator unable to tell what was removed.
func TestClearHistoryContinuesPastAFailure(t *testing.T) {
	campaigns, entries := seedForReset(campaign.StatusStopped)
	wiper := &fakeWiper{err: errBoom}
	uc := NewClearHistoryUseCase(campaigns, entries, wiper)

	prepared, _ := uc.PrepareClearHistory("camp-1")
	out, err := uc.ConfirmClearHistory(uwc.ClearHistoryInput{
		CampaignID: "camp-1", ClearCode: prepared.ClearCode,
	})
	if err != nil {
		t.Fatalf("a wipe failure aborted the whole clear: %v", err)
	}
	if out.DeletedCount != 0 {
		t.Fatalf("deleted = %d, want 0 when every wipe failed", out.DeletedCount)
	}
}
