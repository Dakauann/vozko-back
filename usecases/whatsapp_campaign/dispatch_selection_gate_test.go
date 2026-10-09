package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
)

func TestAPlainCampaignStarts(t *testing.T) {
	campaigns := newMockCampaignRepo()
	campaigns.campaigns["camp-1"] = &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", Status: wc.CampaignStatusStopped}
	uc := NewDispatchCampaignUseCase(nil, campaigns, newMockEntryRepo(), nil, nil)
	if err := uc.Dispatch(wc.DispatchCampaignInput{CampaignID: "camp-1", Action: wc.CampaignActionStart}); err != nil {
		t.Fatalf("Dispatch = %v", err)
	}
	if got := campaigns.campaigns["camp-1"].Status; got != wc.CampaignStatusRunning {
		t.Fatalf("status = %s, want running", got)
	}
}

func TestASelectionSendStartsThroughItsReviewOrResumes(t *testing.T) {
	cases := []struct {
		name     string
		from     wc.Status
		reviewed bool
		err      error
		want     wc.Status
	}{
		{name: "the campaigns page starts a stopped send", from: wc.CampaignStatusStopped, err: campaign.ErrSelectionStartNeedsReview, want: wc.CampaignStatusStopped},
		{name: "the review starts a stopped send", from: wc.CampaignStatusStopped, reviewed: true, want: wc.CampaignStatusRunning},
		{name: "a paused send resumes", from: wc.CampaignStatusPaused, want: wc.CampaignStatusRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			campaigns := newMockCampaignRepo()
			campaigns.campaigns["camp-1"] = &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", Status: tc.from, Source: campaign.SourceLeadSelection}
			uc := NewDispatchCampaignUseCase(nil, campaigns, newMockEntryRepo(), nil, nil)

			err := uc.Dispatch(wc.DispatchCampaignInput{CampaignID: "camp-1", Action: wc.CampaignActionStart, Reviewed: tc.reviewed})

			if !errors.Is(err, tc.err) {
				t.Fatalf("Dispatch = %v, want %v", err, tc.err)
			}
			if got := campaigns.campaigns["camp-1"].Status; got != tc.want {
				t.Fatalf("status = %s, want %s", got, tc.want)
			}
		})
	}
}
