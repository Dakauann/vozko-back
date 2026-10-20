package smartcampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/campaign"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	"vozko/domain/working_hours"
)

var ctx = context.Background()

func sendHours() *working_hours.Spec {
	open := []working_hours.Window{{Start: "08:00", End: "20:00"}}
	return &working_hours.Spec{
		Timezone: "America/Sao_Paulo",
		Days:     map[string][]working_hours.Window{"mon": open, "tue": open, "wed": open, "thu": open, "fri": open, "sat": open},
	}
}

type rig struct {
	t        *testing.T
	store    *store
	health   *fakeHealth
	consumer *fakeConsumer
	queue    *fakeQueue
	caps     *fakeCaps
	notifier *fakeNotifier
	clock    *fakeClock
	engine   *Engine
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		t: t, store: newStore(), health: newHealth(), consumer: newConsumer(), queue: &fakeQueue{},
		caps: &fakeCaps{}, notifier: &fakeNotifier{},
		clock: &fakeClock{now: time.Date(2026, 10, 16, 10, 0, 0, 0, brasilia)},
	}
	r.engine = NewEngine(Deps{
		Campaigns: r.store, Entries: r.store, Policies: r.store, States: r.store, Waves: r.store,
		Health: r.health, Consumer: r.consumer, Queue: r.queue, Caps: r.caps, Notifier: r.notifier,
		Locks: &memLocks{}, Now: r.clock.Now,
	})
	return r
}

func (r *rig) smart(id string, contacts int, preset sc.Preset) *wc.Campaign {
	policy := sc.PresetPolicy(preset, sendHours())
	return r.store.addCampaign(id, contacts, &policy)
}

func (r *rig) start(id string) {
	r.t.Helper()
	if err := r.engine.Start(ctx, id); err != nil {
		r.t.Fatalf("start %s: %v", id, err)
	}
}

func (r *rig) tick(after time.Duration) {
	r.t.Helper()
	r.clock.advance(after)
	if err := r.engine.Tick(ctx); err != nil {
		r.t.Fatalf("tick: %v", err)
	}
}

func (r *rig) status(id string) campaign.Status {
	return r.store.campaigns[id].Status
}

func (r *rig) wave(id string, number int) sc.Wave {
	r.t.Helper()
	waves := r.store.waves[id]
	if len(waves) < number {
		r.t.Fatalf("%s has %d waves, want wave %d", id, len(waves), number)
	}
	return waves[number-1]
}

func (r *rig) drainAndObserve(id string, number int, failEvery, code int) {
	r.t.Helper()
	r.store.process(id, number, failEvery, code)
	r.tick(time.Minute)
	if got := r.wave(id, number).State; got != sc.Drained {
		r.t.Fatalf("wave %d state = %s, want drained", number, got)
	}
	r.tick(2 * time.Hour)
}

func TestStartReleasesTheFirstWaveOnly(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 1000, sc.Standard)

	r.start("c1")

	if r.status("c1") != campaign.StatusRunning {
		t.Fatalf("status = %s, want running", r.status("c1"))
	}
	w := r.wave("c1", 1)
	if w.Size != 200 || w.State != sc.Enqueued {
		t.Fatalf("wave 1 = %+v, want 200 enqueued", w)
	}
	if got := len(r.queue.enqueued["c1"]); got != 200 {
		t.Fatalf("enqueued %d messages, want only the first wave", got)
	}
	if !r.consumer.subscribed["c1"] {
		t.Fatal("the consumer was not subscribed before enqueueing")
	}
}

func TestStartRefusesWhatItDoesNotManage(t *testing.T) {
	r := newRig(t)
	r.store.addCampaign("plain", 100, nil)
	if err := r.engine.Start(ctx, "plain"); !errors.Is(err, sc.ErrNotSmart) {
		t.Fatalf("start of a campaign without policy = %v, want ErrNotSmart", err)
	}
	r.smart("c1", 100, sc.Standard)
	r.start("c1")
	if err := r.engine.Start(ctx, "c1"); !errors.Is(err, campaign.ErrAlreadyRunning) {
		t.Fatalf("second start = %v, want ErrAlreadyRunning", err)
	}
}

func TestStartIsRefusedWhileAHoldSignalIsActive(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 100, sc.Standard)
	r.health.templates["tpl-c1"] = sc.TemplateState{Status: "PAUSED", Quality: "GREEN", Category: "UTILITY"}

	err := r.engine.Start(ctx, "c1")

	var blocked *sc.BlockedError
	if !errors.As(err, &blocked) || blocked.Decision.Action != sc.Stop {
		t.Fatalf("start = %v, want a blocked error carrying the decision", err)
	}
	if r.status("c1") != campaign.StatusStopped || r.store.waveCount("c1") != 0 {
		t.Fatal("a refused start changed the campaign")
	}
}

func TestAWaveIsObservedBeforeTheNextOneGrows(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")

	r.store.processSome("c1", 1, 150)
	r.tick(time.Minute)
	if r.wave("c1", 1).State != sc.Enqueued {
		t.Fatal("a wave with pending contacts was marked drained")
	}

	r.store.process("c1", 1, 0, 0)
	r.tick(time.Minute)
	w := r.wave("c1", 1)
	if w.State != sc.Drained || !w.ObserveUntil.Equal(r.clock.now.Add(2*time.Hour)) {
		t.Fatalf("wave 1 = %+v, want drained and observed for 2h", w)
	}

	r.tick(time.Hour)
	if r.store.waveCount("c1") != 1 {
		t.Fatal("the next wave left before the observation ended")
	}

	r.tick(time.Hour)
	if got := r.wave("c1", 1); got.State != sc.Decided || got.Decision.Action != sc.Continue {
		t.Fatalf("wave 1 = %+v, want decided continue", got)
	}
	if got := r.wave("c1", 2).Size; got != 400 {
		t.Fatalf("wave 2 size = %d, want 400", got)
	}
}

func TestAFailingListHalvesTheNextWave(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")

	r.drainAndObserve("c1", 1, 10, sc.CodeUndeliverable)

	if got := r.wave("c1", 1).Decision.Action; got != sc.SlowDown {
		t.Fatalf("wave 1 decision = %s, want slow down", got)
	}
	if got := r.wave("c1", 2).Size; got != 100 {
		t.Fatalf("wave 2 size = %d, want 100", got)
	}
}

func TestOutsideSendHoursTheCampaignWaitsForTheOpening(t *testing.T) {
	r := newRig(t)
	r.clock.now = time.Date(2026, 10, 16, 18, 30, 0, 0, brasilia)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")

	r.drainAndObserve("c1", 1, 0, 0)

	st := r.store.states["c1"]
	opening := time.Date(2026, 10, 17, 8, 0, 0, 0, brasilia)
	if r.store.waveCount("c1") != 1 || st.Waiting == nil || !st.DueAt.Equal(opening) {
		t.Fatalf("state %+v, want waiting until %v with no new wave", st, opening)
	}

	r.tick(time.Hour)
	if r.store.waveCount("c1") != 1 {
		t.Fatal("a wave left outside the send hours")
	}

	r.clock.now = opening
	r.tick(0)
	if r.store.waveCount("c1") != 2 || r.store.states["c1"].Waiting != nil {
		t.Fatal("the campaign did not continue at the opening")
	}
}

func TestUnreadableHealthWaitsThenPauses(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")
	r.health.freshFails = true

	r.drainAndObserve("c1", 1, 0, 0)
	if r.store.waveCount("c1") != 1 || r.status("c1") != campaign.StatusRunning {
		t.Fatal("a wave was released without a fresh health read")
	}

	for i := 0; i < 70 && r.status("c1") == campaign.StatusRunning; i++ {
		r.tick(5 * time.Minute)
	}
	if r.status("c1") != campaign.StatusPaused {
		t.Fatalf("status = %s, want paused after the max observe wait", r.status("c1"))
	}
	if hold := r.store.states["c1"].Hold; hold == nil || hold.Decision.Findings[len(hold.Decision.Findings)-1].Rule != sc.RuleHealthUnavailable {
		t.Fatalf("hold = %+v, want health unavailable", hold)
	}
}

func TestASpamLimitErrorStopsAtOnce(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 1000, sc.Standard)
	r.start("c1")

	r.store.failOne("c1", 1, sc.CodeSpamLimit)
	r.engine.EntryFailed(ctx, "c1", sc.CodeSpamLimit)

	if r.status("c1") != campaign.StatusStopped {
		t.Fatalf("status = %s, want stopped", r.status("c1"))
	}
	if last := r.consumer.calls[len(r.consumer.calls)-1]; last != "stop:c1" {
		t.Fatalf("last consumer call = %s, want the queue dropped", last)
	}
	if len(r.notifier.held) != 1 || r.wave("c1", 1).State != sc.Decided {
		t.Fatal("the hold was not recorded and notified")
	}
}

func TestACommonFailureIsDebounced(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 1000, sc.Standard)
	r.start("c1")
	reads := r.health.freshReads

	for i := 0; i < 5; i++ {
		r.engine.EntryFailed(ctx, "c1", sc.CodeUndeliverable)
	}

	if r.status("c1") != campaign.StatusRunning || r.health.freshReads != reads {
		t.Fatal("common failures triggered more than one cheap live check")
	}
}

func TestFailuresOfCampaignsWithoutPolicyAreIgnored(t *testing.T) {
	r := newRig(t)
	r.store.addCampaign("plain", 100, nil)
	r.store.campaigns["plain"].Status = campaign.StatusRunning

	r.engine.EntryFailed(ctx, "plain", sc.CodeSpamLimit)
	if err := r.engine.HealthChanged(ctx, "tpl-plain", ""); err != nil {
		t.Fatal(err)
	}

	if r.status("plain") != campaign.StatusRunning || len(r.consumer.calls) != 0 {
		t.Fatal("the engine touched a campaign without a policy")
	}
}

func TestAYellowTemplatePausesAtOnceAndResumeWaitsForGreen(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")
	r.store.processSome("c1", 1, 120)

	r.health.templates["tpl-c1"] = sc.TemplateState{Status: "APPROVED", Quality: "YELLOW", Category: "UTILITY"}
	if err := r.engine.HealthChanged(ctx, "tpl-c1", ""); err != nil {
		t.Fatal(err)
	}
	if r.status("c1") != campaign.StatusPaused {
		t.Fatalf("status = %s, want paused", r.status("c1"))
	}

	err := r.engine.Start(ctx, "c1")
	if !errors.Is(err, sc.ErrStillUnhealthy) || r.status("c1") != campaign.StatusPaused {
		t.Fatalf("resume while yellow = %v, want refused", err)
	}
	if len(r.notifier.resumed) != 1 {
		t.Fatalf("a refused resume must keep the hold alert open, resumed %v", r.notifier.resumed)
	}

	r.health.templates["tpl-c1"] = sc.TemplateState{Status: "APPROVED", Quality: "GREEN", Category: "UTILITY"}
	r.start("c1")
	if len(r.notifier.resumed) != 2 || r.notifier.resumed[1] != "c1" {
		t.Fatalf("a resume closes the hold alert so the next hold notifies again, resumed %v", r.notifier.resumed)
	}

	if pending, _ := r.store.PendingInWave("c1", 1); pending != 0 {
		t.Fatalf("%d unsent contacts stayed in the interrupted wave", pending)
	}
	if got := r.wave("c1", 2).Size; got != 50 {
		t.Fatalf("wave after the hold = %d, want the minimum 50", got)
	}
}

func TestAManualPauseResumesTheSameWave(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")
	r.store.processSome("c1", 1, 80)
	r.store.campaigns["c1"].Status = campaign.StatusPaused
	r.consumer.subscribed["c1"] = false

	r.start("c1")

	if r.store.waveCount("c1") != 1 || r.wave("c1", 1).State != sc.Enqueued {
		t.Fatal("a manual resume did not continue the same wave")
	}
	if len(r.notifier.resumed) != 2 {
		t.Fatalf("continuing the same wave is also a resume, resumed %v", r.notifier.resumed)
	}
	if last := r.consumer.calls[len(r.consumer.calls)-1]; last != "subscribe:c1" {
		t.Fatalf("last consumer call = %s, want the consumer attached again", last)
	}
}

func TestAStoppedCampaignResumesWithTheRestReturnedToThePool(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")
	r.store.processSome("c1", 1, 80)
	r.store.campaigns["c1"].Status = campaign.StatusStopped

	r.start("c1")

	if got := r.wave("c1", 1); got.State != sc.Decided {
		t.Fatalf("interrupted wave = %+v, want closed", got)
	}
	if got := r.wave("c1", 2).Size; got != 200 {
		t.Fatalf("wave after a manual stop = %d, want the regular size", got)
	}
	if r.status("c1") != campaign.StatusRunning {
		t.Fatal("the campaign did not resume")
	}
}

func TestTheCampaignCompletesWhenNothingIsLeft(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 150, sc.Standard)
	r.start("c1")

	r.drainAndObserve("c1", 1, 0, 0)

	if r.status("c1") != campaign.StatusCompleted {
		t.Fatalf("status = %s, want completed", r.status("c1"))
	}
	if got := r.wave("c1", 1).Decision.Action; got != sc.Complete {
		t.Fatalf("last wave decision = %s, want complete", got)
	}
}

func TestAFailedEnqueueIsRetriedWithoutANewWave(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 1000, sc.Standard)
	r.queue.fails = 1

	if err := r.engine.Start(ctx, "c1"); err == nil {
		t.Fatal("the enqueue failure was swallowed")
	}
	if r.wave("c1", 1).State != sc.Released {
		t.Fatal("a wave that was not enqueued left the released state")
	}

	r.tick(time.Minute)

	if r.store.waveCount("c1") != 1 || r.wave("c1", 1).State != sc.Enqueued || len(r.queue.enqueued["c1"]) != 200 {
		t.Fatal("the released wave was not enqueued again on the tick")
	}
}

func TestAHoldWhoseConsumerDoesNotDetachIsRetried(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 1000, sc.Standard)
	r.start("c1")
	r.consumer.pauseFails = 1
	r.health.phones["phone-c1"] = sc.PhoneState{Status: businessphone.StatusRestricted}

	if err := r.engine.HealthChanged(ctx, "", "phone-c1"); err == nil {
		t.Fatal("the failed detach was swallowed")
	}
	if r.status("c1") != campaign.StatusRunning {
		t.Fatal("the status changed while the consumer was still attached")
	}

	r.tick(time.Minute)

	if r.status("c1") != campaign.StatusPaused {
		t.Fatalf("status = %s, want paused on the retry", r.status("c1"))
	}
}

func TestNoMonthlyRoomPauses(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 1000, sc.Standard)
	r.caps.usage = &balance.SendCapUsage{Cap: balance.MonthlySendCap{Limit: 100}, Used: 100}

	r.start("c1")

	if r.status("c1") != campaign.StatusPaused || r.store.waveCount("c1") != 0 {
		t.Fatal("a campaign without monthly room released a wave")
	}
}

func TestAStuckSlowDownPausesForAPerson(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 5000, sc.Standard)
	r.start("c1")

	for number := 1; r.status("c1") == campaign.StatusRunning && number < 10; number++ {
		r.drainAndObserve("c1", number, 10, sc.CodeUndeliverable)
	}

	if r.status("c1") != campaign.StatusPaused {
		t.Fatalf("status = %s, want paused", r.status("c1"))
	}
	primary, _ := r.store.states["c1"].Hold.Decision.Primary()
	if primary.Rule != sc.RulePersistentSlowDown {
		t.Fatalf("hold rule = %s, want persistent slow down", primary.Rule)
	}
}

func TestOverviewTellsWhatHappensNext(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 2000, sc.Standard)
	r.start("c1")
	r.store.process("c1", 1, 0, 0)
	r.tick(time.Minute)

	view, err := r.engine.Overview("c1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Progress.Phase != sc.PhaseObserving || !view.Progress.Until.Equal(r.wave("c1", 1).ObserveUntil) || len(view.Waves) != 1 {
		t.Fatalf("overview = %+v, want observing until the end of wave 1", view.Progress)
	}
}

func TestANewRoundIsNotGrownFromTheLastWave(t *testing.T) {
	r := newRig(t)
	r.smart("c1", 1000, sc.Standard)
	r.start("c1")
	r.drainAndObserve("c1", 1, 0, 0)
	r.drainAndObserve("c1", 2, 0, 0)
	r.drainAndObserve("c1", 3, 0, 0)
	if r.status("c1") != campaign.StatusCompleted {
		t.Fatalf("status = %s, want completed", r.status("c1"))
	}
	for _, e := range r.store.entries["c1"] {
		e.status, e.wave = "PENDING", 0
	}
	r.store.campaigns["c1"].Status = campaign.StatusStopped

	r.start("c1")

	if got := r.wave("c1", 4).Size; got != 200 {
		t.Fatalf("first wave of the new round = %d, want 200", got)
	}
}
