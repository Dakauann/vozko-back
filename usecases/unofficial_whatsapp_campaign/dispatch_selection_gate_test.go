package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func TestAnUnofficialSelectionSendStartsThroughItsReviewOrResumes(t *testing.T) {
	cases := []struct {
		name     string
		from     campaign.Status
		reviewed bool
		err      error
		want     campaign.Status
	}{
		{name: "the campaigns page starts a stopped send", from: campaign.StatusStopped, err: campaign.ErrSelectionStartNeedsReview, want: campaign.StatusStopped},
		{name: "the review starts a stopped send", from: campaign.StatusStopped, reviewed: true, want: campaign.StatusRunning},
		{name: "a paused send resumes", from: campaign.StatusPaused, want: campaign.StatusRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, campaigns, entries, _, consumer := newDispatchHarness(t)
			seedStopped(campaigns, entries)
			c, _ := campaigns.FindByID("camp-1")
			c.Source, c.Status = campaign.SourceLeadSelection, tc.from
			campaigns.put(c)

			err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-1", Action: campaign.ActionStart, Reviewed: tc.reviewed})

			if !errors.Is(err, tc.err) {
				t.Fatalf("Dispatch = %v, want %v", err, tc.err)
			}
			if got, _ := campaigns.FindByID("camp-1"); got.Status != tc.want {
				t.Fatalf("status = %s, want %s", got.Status, tc.want)
			}
			if tc.err != nil && consumer.subscribed["camp-1"] {
				t.Fatalf("a refused start subscribed the consumer")
			}
		})
	}
}
