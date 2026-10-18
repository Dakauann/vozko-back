package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type stubbornConsumer struct {
	*stubConsumer
	pauseErr error
	stopErr  error
}

func (s *stubbornConsumer) PauseCampaignConsumer(id string) error {
	if s.pauseErr != nil {
		return s.pauseErr
	}
	return s.stubConsumer.PauseCampaignConsumer(id)
}

func (s *stubbornConsumer) StopCampaignConsumer(id string) error {
	if s.stopErr != nil {
		return s.stopErr
	}
	return s.stubConsumer.StopCampaignConsumer(id)
}

func runningCampaign(t *testing.T, consumer *stubbornConsumer) (uwc.DispatchCampaignUseCase, *fakeCampaignRepo) {
	t.Helper()
	campaigns, entries := newFakeCampaignRepo(), newFakeEntryRepo()
	seedStopped(campaigns, entries)
	gateway := &fakeGateway{instance: &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected, SendDelayMinMS: 500, SendDelayMaxMS: 600}}
	uc := NewDispatchCampaignUseCase(campaigns, entries, gateway, consumer, noopQueuePub{}, newFakeShared())
	if err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-1", Action: campaign.ActionStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return uc, campaigns
}

func TestAPauseWhoseConsumerKeepsRunningIsRevertedAndReported(t *testing.T) {
	consumer := &stubbornConsumer{stubConsumer: newStubConsumer(), pauseErr: errBoom}
	uc, campaigns := runningCampaign(t, consumer)

	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-1", Action: campaign.ActionPause})

	if !errors.Is(err, errBoom) {
		t.Fatalf("pause = %v, want the consumer failure", err)
	}
	if c, _ := campaigns.FindByID("camp-1"); c.Status != campaign.StatusRunning {
		t.Fatalf("status = %q, want RUNNING because the consumer is still attached", c.Status)
	}
}

func TestAStopWhoseQueueSurvivesIsRevertedAndReported(t *testing.T) {
	consumer := &stubbornConsumer{stubConsumer: newStubConsumer(), stopErr: errBoom}
	uc, campaigns := runningCampaign(t, consumer)

	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-1", Action: campaign.ActionStop})

	if !errors.Is(err, errBoom) {
		t.Fatalf("stop = %v, want the consumer failure", err)
	}
	if c, _ := campaigns.FindByID("camp-1"); c.Status != campaign.StatusRunning {
		t.Fatalf("status = %q, want RUNNING", c.Status)
	}
}

func TestTheBreakerOnlyPausesCampaignsWhoseConsumerDetached(t *testing.T) {
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{ID: "a", InstanceID: "inst-1", Status: campaign.StatusRunning})
	consumer := &stubbornConsumer{stubConsumer: newStubConsumer(), pauseErr: errBoom}

	paused, err := NewPauseCampaignsForInstanceUseCase(campaigns, consumer).Execute(context.Background(), "inst-1", "WhatsApp restricted this number")

	if err != nil || paused != 0 {
		t.Fatalf("paused %d, err %v", paused, err)
	}
	if c, _ := campaigns.FindByID("a"); c.Status != campaign.StatusRunning || campaigns.reason("a") != "" {
		t.Fatalf("status %q reason %q, want untouched while the consumer still runs", c.Status, campaigns.reason("a"))
	}
}

type scheduleLedger struct {
	claimable map[string]bool
	claimed   []string
}

func (s *scheduleLedger) ClaimScheduledStart(campaignID string, _ time.Time) (bool, error) {
	if !s.claimable[campaignID] {
		return false, nil
	}
	s.claimable[campaignID] = false
	s.claimed = append(s.claimed, campaignID)
	return true, nil
}

type dueRepo struct {
	*fakeCampaignRepo
	due []*uwc.Campaign
}

func (d dueRepo) ListScheduledToStart(time.Time, int) ([]*uwc.Campaign, error) { return d.due, nil }

type recordingDispatch struct{ started []string }

func (r *recordingDispatch) Dispatch(_ context.Context, input uwc.DispatchCampaignInput) error {
	r.started = append(r.started, input.CampaignID)
	return nil
}

func TestAScheduleFiresOnlyOnceForTheCampaignsItClaims(t *testing.T) {
	ledger := &scheduleLedger{claimable: map[string]bool{"a": true, "b": false}}
	dispatch := &recordingDispatch{}
	job := NewScheduleStartJob(dueRepo{fakeCampaignRepo: newFakeCampaignRepo(), due: []*uwc.Campaign{{ID: "a"}, {ID: "b"}}}, dispatch, ledger)

	_ = job.StartScheduledCampaigns()
	_ = job.StartScheduledCampaigns()

	if len(dispatch.started) != 1 || dispatch.started[0] != "a" {
		t.Fatalf("started %v, want a once", dispatch.started)
	}
}
