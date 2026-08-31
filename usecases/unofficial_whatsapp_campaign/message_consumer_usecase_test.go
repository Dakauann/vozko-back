package unofficial_whatsapp_campaign

import (
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/campaignqueue"
)

type harness struct {
	consumer  *messageConsumerUseCase
	campaigns *fakeCampaignRepo
	entries   *fakeEntryRepo
	gateway   *fakeGateway
	sender    *fakeSender
	spam      *fakeSpam
	assigner  *fakeAssigner
	metrics   *fakeMetrics
	workflows *fakeWorkflows
	pauser    *fakePauser
	shared    *fakeShared
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	connected := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-1",
		Status: uw.StatusConnected, DailySendCap: 1000,
		SendDelayMinMS: 500, SendDelayMaxMS: 600,
	}

	h := &harness{
		campaigns: newFakeCampaignRepo(),
		entries:   newFakeEntryRepo(),
		gateway:   &fakeGateway{instance: connected},
		sender:    &fakeSender{},
		spam:      &fakeSpam{skip: map[string]bool{}},
		assigner:  &fakeAssigner{},
		metrics:   &fakeMetrics{},
		workflows: &fakeWorkflows{},
		pauser:    &fakePauser{},
		shared:    newFakeShared(),
	}

	uc := &messageConsumerUseCase{deps: ConsumerDeps{
		QueueSub: noopQueueSub{}, QueuePub: noopQueuePub{}, Shared: h.shared,
		Campaigns: h.campaigns, Entries: h.entries, Instances: h.gateway,
		Sender: h.sender, Budget: NewSendBudget(h.shared), Spam: h.spam,
		Assignments: h.assigner, Metrics: h.metrics, Workflows: h.workflows,
		PauseAll: h.pauser,
	}}
	uc.attachRunner()
	h.consumer = uc
	return h
}

func (h *harness) seed(t *testing.T) (campID, entryID string) {
	t.Helper()
	campID, entryID = "camp-1", "entry-1"
	h.campaigns.put(&uwc.Campaign{
		ID: campID, WorkspaceID: "ws-1", InstanceID: "inst-1",
		Name: "cobranca", Status: campaign.StatusRunning,
		Message:        uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"bom dia {{1}}"}},
		SendDelayMinMS: 500, SendDelayMaxMS: 600,
		EnableWorkflow: true,
	})
	h.entries.put(&uwc.Entry{
		ID: entryID, CampaignID: campID, WorkspaceID: "ws-1",
		LeadID: "lead-1", Number: "5584999990001",
		Status: campaign.SendStatusPending, Variables: []string{"Ana"},
	})
	return
}

func (h *harness) run(campID, entryID string) campaignqueue.Result {
	return h.consumer.handle(campaignqueue.Message{
		CampaignID: campID, EntryID: entryID, PhoneNumber: "5584999990001",
	})
}

// ---------------------------------------------------------------- happy path

func TestSuccessfulSendRecordsEverythingItOwes(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)

	if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", got.Outcome)
	}

	if h.sender.count() != 1 {
		t.Fatalf("sends = %d, want 1", h.sender.count())
	}
	// Variables rendered before the send, never after.
	if body := h.sender.last().Text; body != "bom dia Ana" {
		t.Errorf("body = %q, want the rendered text", body)
	}
	if got := h.entries.get(entryID).Status; got != campaign.SendStatusSent {
		t.Errorf("status = %q, want SENT", got)
	}
	// Assignment at SEND time is what makes a campaign a set of owned
	// conversations rather than a fire-and-forget log.
	if len(h.assigner.calls) != 1 {
		t.Errorf("assignments = %d, want 1", len(h.assigner.calls))
	}
	if len(h.spam.recorded) != 1 {
		t.Errorf("cooldown records = %d, want 1", len(h.spam.recorded))
	}
	if len(h.metrics.recorded) != 1 {
		t.Errorf("metrics = %d, want 1", len(h.metrics.recorded))
	}
	if len(h.workflows.fired) != 1 {
		t.Errorf("workflow triggers = %d, want 1", len(h.workflows.fired))
	}
	// The entry has to link to a real conversation, or the campaign row is not
	// clickable through to a transcript.
	if conv := h.entries.get(entryID).ConversationID; conv == "" {
		t.Error("entry has no conversation id")
	}
}

// The workflow trigger is gated on the campaign switch.
func TestWorkflowTriggerRespectsTheCampaignSwitch(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	c, _ := h.campaigns.FindByID(campID)
	c.EnableWorkflow = false
	h.campaigns.put(c)

	h.run(campID, entryID)
	if len(h.workflows.fired) != 0 {
		t.Fatalf("a workflow fired with the switch off")
	}
}

// ---------------------------------------------------------------- idempotency

// At-least-once delivery means the same message can arrive twice. The second
// must be a no-op, not a second message to a customer.
func TestRedeliveryDoesNotSendTwice(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)

	h.run(campID, entryID)
	second := h.run(campID, entryID)

	if h.sender.count() != 1 {
		t.Fatalf("sends = %d after a redelivery, want 1", h.sender.count())
	}
	if second.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("redelivery outcome = %v, want Drop", second.Outcome)
	}
}

// ---------------------------------------------------------------- ban safety

// A dead session pauses every campaign on the number and leaves the entry
// PENDING: the message was never sent, and failing it would make a resumed
// campaign skip someone who was never contacted.
func TestDeadSessionPausesAndKeepsTheEntryPending(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.gateway.instance.Status = uw.StatusDisconnected

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("outcome = %v, want RetryLater", got.Outcome)
	}
	if h.pauser.calls != 1 {
		t.Errorf("circuit breaker fired %d times, want 1", h.pauser.calls)
	}
	if status := h.entries.get(entryID).Status; status != campaign.SendStatusPending {
		t.Errorf("status = %q, want PENDING", status)
	}
	if h.sender.count() != 0 {
		t.Error("a message was sent from a dead session")
	}
}

func TestWhatsAppRestrictionPauses(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	blocked := false
	h.gateway.instance.Restriction = uw.Restriction{CanSendNewChats: &blocked}

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("outcome = %v, want RetryLater", got.Outcome)
	}
	if h.pauser.calls != 1 {
		t.Errorf("circuit breaker fired %d times, want 1", h.pauser.calls)
	}
}

// A number that is not on WhatsApp is a LIST-QUALITY fact, never a failure.
func TestNumberNotOnWhatsAppIsSkippedNotFailed(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.gateway.checkResult = []uw.NumberCheck{{Query: "5584999990001", IsOnWhatsApp: false}}

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
	if status := h.entries.get(entryID).Status; status != campaign.SendStatusSkippedNotOnWhatsApp {
		t.Fatalf("status = %q, want SKIPPED_NOT_ON_WHATSAPP", status)
	}
	if h.sender.count() != 0 {
		t.Error("a message was sent to a number that is not on WhatsApp")
	}
}

// A dead number must not burn the day's allowance.
func TestSkippedNumberReturnsItsBudget(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.gateway.checkResult = []uw.NumberCheck{{Query: "5584999990001", IsOnWhatsApp: false}}

	h.run(campID, entryID)
	if used := NewSendBudget(h.shared).UsedToday("inst-1"); used != 0 {
		t.Fatalf("a skipped number consumed %d of the daily budget", used)
	}
}

func TestSpamWindowSkips(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.spam.skip["lead-1"] = true

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
	if status := h.entries.get(entryID).Status; status != campaign.SendStatusNotEligiblePossibleSpam {
		t.Fatalf("status = %q, want NOT_ELIGIBLE_POSSIBLE_SPAM", status)
	}
	if h.sender.count() != 0 {
		t.Error("a message was sent inside the cooldown window")
	}
}

// Out of daily budget defers the work; it is not a failure and the entry stays
// pending so the next window picks it up.
func TestDailyCapDefersRatherThanFails(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	c, _ := h.campaigns.FindByID(campID)
	c.DailyCap = 1
	h.campaigns.put(c)

	// Burn the single slot.
	NewSendBudget(h.shared).TryConsumeDaily("inst-1", 1)

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("outcome = %v, want RetryLater", got.Outcome)
	}
	if status := h.entries.get(entryID).Status; status != campaign.SendStatusPending {
		t.Fatalf("status = %q, want PENDING", status)
	}
}

// The warmup ramp actually gates a send, not just a display value.
func TestWarmupCapGatesSends(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	start := time.Now().UTC()
	h.gateway.instance.WarmupStartedAt = &start
	h.gateway.instance.DailySendCap = 2100 // day 1 ramps to 100

	budget := NewSendBudget(h.shared)
	for i := 0; i < 100; i++ {
		budget.TryConsumeDaily("inst-1", 100)
	}

	if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("outcome = %v, want RetryLater once the warmup cap is spent", got.Outcome)
	}
}

// ---------------------------------------------------------------- number check caching

func TestAFreshCheckIsNotRepeated(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)

	checked := time.Now().UTC()
	e := h.entries.get(entryID)
	e.JID = "5584999990001@s.whatsapp.net"
	e.CheckedAt = &checked
	h.entries.put(e)

	h.run(campID, entryID)
	if h.gateway.checkCalls != 0 {
		t.Fatalf("a fresh check was repeated (%d provider calls)", h.gateway.checkCalls)
	}
}

func TestAStaleCheckIsRefreshed(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)

	stale := time.Now().UTC().Add(-uwc.NumberCheckTTL - time.Hour)
	e := h.entries.get(entryID)
	e.JID = "5584999990001@s.whatsapp.net"
	e.CheckedAt = &stale
	h.entries.put(e)

	h.run(campID, entryID)
	if h.gateway.checkCalls != 1 {
		t.Fatalf("a stale check was not refreshed (%d provider calls)", h.gateway.checkCalls)
	}
}

// ---------------------------------------------------------------- failures

func TestMissingEntryIsDroppedNotRequeued(t *testing.T) {
	h := newHarness(t)
	campID, _ := h.seed(t)

	// Requeuing a deleted entry would spin forever and the campaign would never
	// complete.
	if got := h.run(campID, "gone"); got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
}

func TestMissingCampaignIsRequeued(t *testing.T) {
	h := newHarness(t)
	_, entryID := h.seed(t)
	if got := h.run("gone", entryID); got.Outcome != campaignqueue.OutcomeRequeue {
		t.Fatalf("outcome = %v, want Requeue", got.Outcome)
	}
}

func TestSendFailureFailsTheEntry(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.sender.err = errBoom

	got := h.run(campID, entryID)
	if got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
	entry := h.entries.get(entryID)
	if entry.Status != campaign.SendStatusFailed {
		t.Fatalf("status = %q, want FAILED", entry.Status)
	}
	if entry.ErrorMessage == "" {
		t.Error("a failed entry carries no reason")
	}
}

func TestConversationResolutionFailureFailsTheEntry(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.gateway.resolveErr = errBoom

	if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
	if status := h.entries.get(entryID).Status; status != campaign.SendStatusFailed {
		t.Fatalf("status = %q, want FAILED", status)
	}
}

// A number that was removed is not a reason to retry forever.
func TestMissingInstanceFailsTheEntry(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	h.gateway.instance = nil

	if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
}

// ---------------------------------------------------------------- variants

// Deterministic per entry, so a resumed campaign never sends one person two
// different messages.
func TestVariantIsStableAcrossRetries(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seed(t)
	c, _ := h.campaigns.FindByID(campID)
	c.Message.Bodies = []string{"A {{1}}", "B {{1}}", "C {{1}}"}
	h.campaigns.put(c)

	h.run(campID, entryID)
	first := h.sender.last().Text

	// Reset and run again; the same entry must receive the same variant.
	e := h.entries.get(entryID)
	e.Status = campaign.SendStatusPending
	h.entries.put(e)
	h.run(campID, entryID)

	if second := h.sender.last().Text; second != first {
		t.Fatalf("variant drifted between runs: %q then %q", first, second)
	}
}
