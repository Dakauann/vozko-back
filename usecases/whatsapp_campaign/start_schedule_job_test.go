package whatsapp_campaign_usecase

import (
	"errors"
	"testing"
	"time"

	wc "vozko/domain/whatsapp_campaign"
)

type dueCampaigns struct {
	wc.Repository
	due []*wc.Campaign
}

func (d dueCampaigns) ListScheduledToStart(time.Time, int) ([]*wc.Campaign, error) {
	return d.due, nil
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

type recordingDispatch struct {
	started []string
	fail    map[string]error
}

func (r *recordingDispatch) Dispatch(input wc.DispatchCampaignInput) error {
	r.started = append(r.started, input.CampaignID)
	return r.fail[input.CampaignID]
}

func TestAScheduleFiresOnlyForTheCampaignsItClaims(t *testing.T) {
	ledger := &scheduleLedger{claimable: map[string]bool{"a": true, "b": false, "c": true}}
	dispatch := &recordingDispatch{}
	job := NewScheduleStartJob(dispatch, dueCampaigns{due: []*wc.Campaign{{ID: "a"}, {ID: "b"}, {ID: "c"}}}, ledger)

	if err := job.StartScheduledWhatsappCampaigns(); err != nil {
		t.Fatal(err)
	}
	if err := job.StartScheduledWhatsappCampaigns(); err != nil {
		t.Fatal(err)
	}

	if len(dispatch.started) != 2 || dispatch.started[0] != "a" || dispatch.started[1] != "c" {
		t.Fatalf("started %v, want a and c once each", dispatch.started)
	}
}

func TestAFailedScheduledStartDoesNotHoldBackTheOthers(t *testing.T) {
	ledger := &scheduleLedger{claimable: map[string]bool{"a": true, "b": true}}
	dispatch := &recordingDispatch{fail: map[string]error{"a": errors.New("no subscription")}}
	job := NewScheduleStartJob(dispatch, dueCampaigns{due: []*wc.Campaign{{ID: "a"}, {ID: "b"}}}, ledger)

	err := job.StartScheduledWhatsappCampaigns()

	if err == nil || len(dispatch.started) != 2 {
		t.Fatalf("err %v, started %v, want b started and a reported", err, dispatch.started)
	}
}

func TestAnAlreadyRunningCampaignIsNotAnError(t *testing.T) {
	ledger := &scheduleLedger{claimable: map[string]bool{"a": true}}
	dispatch := &recordingDispatch{fail: map[string]error{"a": wc.ErrDispatchCampaignAlreadyRunning}}
	job := NewScheduleStartJob(dispatch, dueCampaigns{due: []*wc.Campaign{{ID: "a"}}}, ledger)

	if err := job.StartScheduledWhatsappCampaigns(); err != nil {
		t.Fatalf("err %v, want nil", err)
	}
}
