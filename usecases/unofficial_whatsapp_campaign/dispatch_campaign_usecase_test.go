package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type stubConsumer struct {
	subscribed map[string]bool
	paused     []string
	stopped    []string
	resumed    []string
	subErr     error
}

func newStubConsumer() *stubConsumer {
	return &stubConsumer{subscribed: map[string]bool{}}
}

func (s *stubConsumer) Start() error { return nil }
func (s *stubConsumer) SubscribeToCampaign(id string) error {
	if s.subErr != nil {
		return s.subErr
	}
	s.subscribed[id] = true
	return nil
}
func (s *stubConsumer) StopCampaignConsumer(id string) error {
	s.stopped = append(s.stopped, id)
	delete(s.subscribed, id)
	return nil
}
func (s *stubConsumer) PauseCampaignConsumer(id string) error {
	s.paused = append(s.paused, id)
	return nil
}
func (s *stubConsumer) ResumeCampaignConsumer(id string) error {
	s.resumed = append(s.resumed, id)
	return nil
}
func (s *stubConsumer) IsSubscribed(id string) bool { return s.subscribed[id] }

func newDispatchHarness(t *testing.T) (uwc.DispatchCampaignUseCase, *fakeCampaignRepo, *fakeEntryRepo, *fakeGateway, *stubConsumer) {
	t.Helper()
	campaigns := newFakeCampaignRepo()
	entries := newFakeEntryRepo()
	gateway := &fakeGateway{instance: &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected,
		SendDelayMinMS: 500, SendDelayMaxMS: 600,
	}}
	consumer := newStubConsumer()

	uc := NewDispatchCampaignUseCase(campaigns, entries, gateway, consumer, noopQueuePub{}, newFakeShared())
	return uc, campaigns, entries, gateway, consumer
}

func seedStopped(campaigns *fakeCampaignRepo, entries *fakeEntryRepo) {
	campaigns.put(&uwc.Campaign{
		ID: "camp-1", WorkspaceID: "ws-1", InstanceID: "inst-1",
		Name: "c", Status: campaign.StatusStopped,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"oi"}},
	})
	entries.put(&uwc.Entry{
		ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-1",
		Number: "5584999990001", Status: campaign.SendStatusPending,
	})
}

func TestStartSubscribesAndRuns(t *testing.T) {
	uc, campaigns, entries, _, consumer := newDispatchHarness(t)
	seedStopped(campaigns, entries)

	if err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-1", Action: campaign.ActionStart,
	}); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	c, _ := campaigns.FindByID("camp-1")
	if c.Status != campaign.StatusRunning {
		t.Fatalf("status = %q, want RUNNING", c.Status)
	}
	if !consumer.subscribed["camp-1"] {
		t.Error("the consumer was not subscribed")
	}
}

// Asking WhatsApp itself, not our cache, is what stops a 40.000-number blast
// starting into a live restriction.
func TestStartRefusesWhenWhatsAppSaysNo(t *testing.T) {
	uc, campaigns, entries, gateway, _ := newDispatchHarness(t)
	seedStopped(campaigns, entries)

	blocked := false
	gateway.limits = &uw.Restriction{CanSendNewChats: &blocked}

	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-1", Action: campaign.ActionStart,
	})
	if !errors.Is(err, uw.ErrRestrictedByWA) {
		t.Fatalf("err = %v, want ErrRestrictedByWA", err)
	}
	// The refusal must leave no trace: a campaign that could not start is still
	// stopped, not half-started.
	c, _ := campaigns.FindByID("camp-1")
	if c.Status != campaign.StatusStopped {
		t.Fatalf("status = %q after a refused start, want STOPPED", c.Status)
	}
}

// An unreadable answer is NOT permission. Fail closed.
func TestStartFailsClosedWhenLimitsCannotBeRead(t *testing.T) {
	uc, campaigns, entries, gateway, _ := newDispatchHarness(t)
	seedStopped(campaigns, entries)
	gateway.limitsErr = errBoom

	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-1", Action: campaign.ActionStart,
	})
	if !errors.Is(err, uw.ErrRestrictedByWA) {
		t.Fatalf("err = %v, want a fail-closed refusal", err)
	}
}

func TestStartRefusesABannedNumber(t *testing.T) {
	uc, campaigns, entries, gateway, _ := newDispatchHarness(t)
	seedStopped(campaigns, entries)
	gateway.instance.Status = uw.StatusBanned

	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-1", Action: campaign.ActionStart,
	})
	var unusable *uwc.InstanceUnusableError
	if !errors.As(err, &unusable) {
		t.Fatalf("err = %v, want an InstanceUnusableError", err)
	}
}

func TestStartTwiceIsRefused(t *testing.T) {
	uc, campaigns, entries, _, _ := newDispatchHarness(t)
	seedStopped(campaigns, entries)

	_ = uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-1", Action: campaign.ActionStart})
	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-1", Action: campaign.ActionStart})
	if !errors.Is(err, campaign.ErrAlreadyRunning) {
		t.Fatalf("err = %v, want ErrAlreadyRunning", err)
	}
}

func TestPauseAndStop(t *testing.T) {
	uc, campaigns, entries, _, consumer := newDispatchHarness(t)
	seedStopped(campaigns, entries)
	_ = uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-1", Action: campaign.ActionStart})

	if err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-1", Action: campaign.ActionPause,
	}); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	if len(consumer.paused) != 1 {
		t.Error("the consumer was not paused")
	}

	if err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-1", Action: campaign.ActionStop,
	}); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if len(consumer.stopped) != 1 {
		t.Error("the consumer was not stopped")
	}
}

// A campaign with nothing pending is finished, not perpetually running: nothing
// would ever close it, because completion is counted per queued message.
func TestStartingAnEmptyCampaignCompletesIt(t *testing.T) {
	uc, campaigns, entries, _, _ := newDispatchHarness(t)
	campaigns.put(&uwc.Campaign{
		ID: "camp-empty", WorkspaceID: "ws-1", InstanceID: "inst-1",
		Status:  campaign.StatusStopped,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"oi"}},
	})
	_ = entries

	if err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-empty", Action: campaign.ActionStart,
	}); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	c, _ := campaigns.FindByID("camp-empty")
	if c.Status != campaign.StatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", c.Status)
	}
}

// A failed subscribe must put the status back, or the campaign is stuck RUNNING
// with nothing queued and can neither complete nor restart.
func TestFailedSubscribeRevertsTheStatus(t *testing.T) {
	uc, campaigns, entries, _, consumer := newDispatchHarness(t)
	seedStopped(campaigns, entries)
	consumer.subErr = errBoom

	if err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{
		CampaignID: "camp-1", Action: campaign.ActionStart,
	}); err == nil {
		t.Fatal("expected the subscribe failure to surface")
	}
	c, _ := campaigns.FindByID("camp-1")
	if c.Status != campaign.StatusStopped {
		t.Fatalf("status = %q after a failed subscribe, want STOPPED", c.Status)
	}
}

// ---------------------------------------------------------------- breaker

// The restriction belongs to the NUMBER, so every campaign on it stops — not
// only the one that noticed.
func TestBreakerPausesEveryCampaignOnTheNumber(t *testing.T) {
	campaigns := newFakeCampaignRepo()
	consumer := newStubConsumer()
	for _, id := range []string{"a", "b"} {
		campaigns.put(&uwc.Campaign{ID: id, InstanceID: "inst-1", Status: campaign.StatusRunning})
	}
	// A campaign on a different number must be untouched.
	campaigns.put(&uwc.Campaign{ID: "other", InstanceID: "inst-2", Status: campaign.StatusRunning})

	uc := NewPauseCampaignsForInstanceUseCase(campaigns, consumer)
	paused, err := uc.Execute(context.Background(), "inst-1", "WhatsApp restricted this number")
	if err != nil {
		t.Fatalf("breaker failed: %v", err)
	}
	if paused != 2 {
		t.Fatalf("paused = %d, want 2", paused)
	}
	for _, id := range []string{"a", "b"} {
		c, _ := campaigns.FindByID(id)
		if c.Status != campaign.StatusPaused {
			t.Errorf("campaign %s = %q, want PAUSED", id, c.Status)
		}
		// The reason is what makes an automatic pause legible; without it an
		// operator restarts straight back into the restriction.
		if campaigns.reason(id) == "" {
			t.Errorf("campaign %s paused with no recorded reason", id)
		}
	}
	if c, _ := campaigns.FindByID("other"); c.Status != campaign.StatusRunning {
		t.Error("a campaign on a different number was paused")
	}
}
