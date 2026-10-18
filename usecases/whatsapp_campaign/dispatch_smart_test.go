package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type fakeSmartStarter struct {
	managed map[string]bool
	err     error
	started []string
}

func (f *fakeSmartStarter) Manages(id string) (bool, error) { return f.managed[id], f.err }

func (f *fakeSmartStarter) Start(_ context.Context, id string) error {
	f.started = append(f.started, id)
	return nil
}

type stubbornConsumer struct {
	*qsConsumer
	pauseErr error
	stopErr  error
}

func (c *stubbornConsumer) PauseCampaignConsumer(string) error { return c.pauseErr }
func (c *stubbornConsumer) StopCampaignConsumer(string) error  { return c.stopErr }

type dispatchRig struct {
	repo     *qsCampaignRepo
	entries  *qsEntryRepo
	pub      *qsQueuePub
	consumer *stubbornConsumer
	smart    *fakeSmartStarter
	dispatch *dispatchCampaignUseCase
}

func newDispatchRig(status wc.Status) *dispatchRig {
	r := &dispatchRig{
		repo: newQSCampaignRepo(), entries: newQSEntryRepo(), pub: newQSQueuePub(),
		consumer: &stubbornConsumer{qsConsumer: newQSConsumer()}, smart: &fakeSmartStarter{managed: map[string]bool{}},
	}
	r.repo.put(&wc.Campaign{ID: "c1", Status: status})
	r.entries.pendingByCampaign["c1"] = []*wce.WhatsAppCampaignEntry{
		{ID: "e1", CampaignID: "c1", Lead: &lead.Lead{Number: "5584999990001"}},
		{ID: "e2", CampaignID: "c1", Lead: &lead.Lead{Number: "5584999990002"}},
	}
	r.dispatch = NewDispatchCampaignUseCase(r.pub, r.repo, r.entries, r.consumer, newQSShared()).(*dispatchCampaignUseCase)
	r.dispatch.SetSmartCampaigns(r.smart)
	r.dispatch.SetLaunchGate(passingGate())
	return r
}

func TestStartingASmartCampaignHandsItToTheWaveEngine(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusStopped)
	r.smart.managed["c1"] = true

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart}); err != nil {
		t.Fatal(err)
	}

	if len(r.smart.started) != 1 || r.pub.count() != 0 || len(r.repo.statusCalls) != 0 {
		t.Fatalf("started %v, published %d, status calls %v, want only the engine involved", r.smart.started, r.pub.count(), r.repo.statusCalls)
	}
}

func TestStartingACampaignWithoutPolicySendsEverythingAsToday(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusStopped)

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart}); err != nil {
		t.Fatal(err)
	}

	if len(r.smart.started) != 0 || r.pub.count() != 2 || r.repo.campaigns["c1"].Status != wc.CampaignStatusRunning {
		t.Fatalf("started %v, published %d, status %s", r.smart.started, r.pub.count(), r.repo.campaigns["c1"].Status)
	}
}

func TestAStartIsRefusedWhenThePolicyCannotBeRead(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusStopped)
	r.smart.err = errors.New("db down")

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart}); err == nil {
		t.Fatal("a start went ahead without knowing whether the campaign sends in waves")
	}
	if r.pub.count() != 0 || r.repo.campaigns["c1"].Status != wc.CampaignStatusStopped {
		t.Fatal("the refused start sent or changed the campaign")
	}
}

func TestAPauseWhoseConsumerKeepsRunningIsRevertedAndReported(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusRunning)
	r.consumer.pauseErr = errors.New("consumer busy")

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionPause}); err == nil {
		t.Fatal("the failed pause was swallowed")
	}
	if got := r.repo.campaigns["c1"].Status; got != wc.CampaignStatusRunning {
		t.Fatalf("status = %s, want running because the consumer is still attached", got)
	}
}

func TestAStopWhoseQueueSurvivesIsRevertedAndReported(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusPaused)
	r.consumer.stopErr = errors.New("broker down")

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStop}); err == nil {
		t.Fatal("the failed stop was swallowed")
	}
	if got := r.repo.campaigns["c1"].Status; got != wc.CampaignStatusPaused {
		t.Fatalf("status = %s, want paused as before", got)
	}
}
