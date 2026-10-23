package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

var runFinishedAt = time.Date(2026, 10, 19, 20, 16, 0, 0, time.UTC)

type stubAutomations struct {
	profile conversation.AutomationProfile
	err     error
}

func (s stubAutomations) EntryAutomation(string, string) (conversation.AutomationProfile, error) {
	return s.profile, s.err
}

type stubAgentRuns struct {
	latest *workflow.AgentCallRun
	err    error
}

func (s stubAgentRuns) LatestForEntry(string, string, string) (*workflow.AgentCallRun, error) {
	return s.latest, s.err
}

type onceClaims struct {
	mu         sync.Mutex
	claimed    map[string]bool
	attempts   map[string]int
	err        error
	releaseErr error
	released   int
}

func (c *onceClaims) ClaimAgentContinuation(runID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return false, c.err
	}
	if c.claimed == nil {
		c.claimed, c.attempts = map[string]bool{}, map[string]int{}
	}
	if c.claimed[runID] {
		return false, nil
	}
	c.claimed[runID] = true
	c.attempts[runID]++
	return true, nil
}

func (c *onceClaims) ReleaseAgentContinuation(runID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.releaseErr != nil {
		return false, c.releaseErr
	}
	if !c.claimed[runID] || c.attempts[runID] >= workflow.MaxAgentContinuationAttempts {
		return false, nil
	}
	c.claimed[runID] = false
	c.released++
	return true, nil
}

type messagesSince struct {
	spoke      bool
	err        error
	askedEntry shared.EntryRef
	askedAfter time.Time
}

func (m *messagesSince) CustomerSpokeAfter(entryID string, entryType shared.EntryType, after time.Time) (bool, error) {
	m.askedEntry, m.askedAfter = shared.EntryRef{EntryID: entryID, EntryType: entryType}, after
	return m.spoke, m.err
}

type recordingContinuer struct {
	mu        sync.Mutex
	open      bool
	windowErr error
	turnErr   error
	turns     []AgentContinuationTurn
	onTurn    func()
}

func (c *recordingContinuer) ContinuationWindowOpen(context.Context, AgentContinuationTurn) (bool, error) {
	return c.open, c.windowErr
}

func (c *recordingContinuer) ContinueAgentTurn(_ context.Context, turn AgentContinuationTurn) error {
	if c.onTurn != nil {
		c.onTurn()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns = append(c.turns, turn)
	return c.turnErr
}

type continuationMetrics struct {
	mu       sync.Mutex
	outcomes []string
}

func (m *continuationMetrics) IncWorkflowRunStarted(string)                      {}
func (m *continuationMetrics) IncWorkflowRunFinished(string)                     {}
func (m *continuationMetrics) ObserveWorkflowStepDuration(string, time.Duration) {}
func (m *continuationMetrics) IncWorkflowWebhookDelivery(string, string)         {}
func (m *continuationMetrics) IncWorkflowReplyLookupFailed()                     {}
func (m *continuationMetrics) IncWorkflowAgentContinuation(outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, outcome)
}

type continuationHarness struct {
	automations stubAutomations
	runs        stubAgentRuns
	claims      *onceClaims
	messages    *messagesSince
	official    *recordingContinuer
	channels    *recordingContinuer
	lockState   *lockState
	metrics     *continuationMetrics
}

func newContinuationHarness() *continuationHarness {
	return &continuationHarness{
		automations: stubAutomations{profile: conversation.AutomationProfile{AgentID: "agent-7", AgentResponsesEnabled: true}},
		runs: stubAgentRuns{latest: &workflow.AgentCallRun{
			RunID: "run-1", WorkflowName: "Matrícula", Status: workflow.RunStatusCompleted,
			AgentResult: map[string]interface{}{"status": "matriculado"}, Error: "run stuck: not updated within timeout",
		}},
		claims:    &onceClaims{},
		messages:  &messagesSince{},
		official:  &recordingContinuer{open: true},
		channels:  &recordingContinuer{open: true},
		lockState: newLockState(),
		metrics:   &continuationMetrics{},
	}
}

func (h *continuationHarness) continuation(t *testing.T) *AgentContinuation {
	t.Helper()
	lock := NewAgentTurnLock(h.lockState)
	lock.poll = time.Millisecond
	c, err := NewAgentContinuation(AgentContinuationDeps{
		Automations: h.automations,
		Runs:        h.runs,
		Claims:      h.claims,
		Messages:    h.messages,
		Continuers:  AgentTurnContinuers{Official: h.official, Channels: h.channels},
		Lock:        lock,
		Metrics:     h.metrics,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func finished(status workflow.RunStatus, hasResult bool) workflow.AgentRunFinished {
	return workflow.AgentRunFinished{
		RunID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: string(shared.EntryTypeTelegram),
		AgentID: "agent-7", Status: status, HasAgentResult: hasResult, FinishedAt: runFinishedAt,
	}
}

func (h *continuationHarness) turns() []AgentContinuationTurn {
	return append(append([]AgentContinuationTurn{}, h.official.turns...), h.channels.turns...)
}

func TestARunThatRepliedToTheAgentGetsOneAgentTurnRightAway(t *testing.T) {
	h := newContinuationHarness()

	outcome := h.continuation(t).Handle(context.Background(), finished(workflow.RunStatusCompleted, true))

	if outcome != "sent" || len(h.channels.turns) != 1 || len(h.official.turns) != 0 {
		t.Fatalf("outcome = %q, channel turns = %d, official turns = %d", outcome, len(h.channels.turns), len(h.official.turns))
	}
	turn := h.channels.turns[0]
	if turn.EntryID != "entry-1" || turn.EntryType != shared.EntryTypeTelegram || turn.AgentID != "agent-7" || turn.WorkspaceID != "ws-1" {
		t.Fatalf("turn = %+v", turn)
	}
	if !strings.Contains(turn.Note, `"Matrícula"`) || !strings.Contains(turn.Note, "retornou") {
		t.Fatalf("note = %q", turn.Note)
	}
	if !h.messages.askedAfter.Equal(runFinishedAt) || h.messages.askedEntry != (shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeTelegram}) {
		t.Fatalf("asked %+v after %v, want this entry after the run ended", h.messages.askedEntry, h.messages.askedAfter)
	}
	if len(h.metrics.outcomes) != 1 || h.metrics.outcomes[0] != "sent" {
		t.Fatalf("metrics = %v", h.metrics.outcomes)
	}
}

func TestAFailedRunGivesTheAgentTheReasonAndForbidsInventingAStatus(t *testing.T) {
	h := newContinuationHarness()
	h.runs.latest.Status = workflow.RunStatusError

	h.continuation(t).Handle(context.Background(), finished(workflow.RunStatusError, false))

	if len(h.channels.turns) != 1 {
		t.Fatalf("turns = %d", len(h.channels.turns))
	}
	note := h.channels.turns[0].Note
	if !strings.Contains(note, "falhou") || !strings.Contains(note, "parado") || !strings.Contains(note, "não invente") {
		t.Fatalf("note = %q", note)
	}
}

func TestOfficialWhatsAppContinuesThroughItsOwnReplyPath(t *testing.T) {
	h := newContinuationHarness()
	ev := finished(workflow.RunStatusCompleted, true)
	ev.EntryType = string(shared.EntryTypeWhatsApp)

	h.continuation(t).Handle(context.Background(), ev)

	if len(h.official.turns) != 1 || len(h.channels.turns) != 0 {
		t.Fatalf("official = %d, channels = %d", len(h.official.turns), len(h.channels.turns))
	}
}

func TestNoTurnWhenTheRunOwesTheAgentNothing(t *testing.T) {
	for name, ev := range map[string]workflow.AgentRunFinished{
		"completed without a reply node": finished(workflow.RunStatusCompleted, false),
		"cancelled":                      finished(workflow.RunStatusCancelled, true),
	} {
		t.Run(name, func(t *testing.T) {
			h := newContinuationHarness()
			if outcome := h.continuation(t).Handle(context.Background(), ev); outcome != "not_due" || len(h.turns()) != 0 {
				t.Fatalf("outcome = %q, turns = %d", outcome, len(h.turns()))
			}
		})
	}
}

func TestNoTurnWhenSomethingCannotBeVerified(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
		mutate  func(h *continuationHarness)
	}{
		{"the automation cannot be read", "automation_unreadable", func(h *continuationHarness) { h.automations.err = errors.New("db down") }},
		{"automation is off", "automation_changed", func(h *continuationHarness) {
			off := false
			h.automations.profile.AutomationEnabled = &off
		}},
		{"another agent took over", "automation_changed", func(h *continuationHarness) { h.automations.profile.AgentID = "agent-2" }},
		{"a workflow was delegated the conversation", "automation_changed", func(h *continuationHarness) {
			h.automations.profile.Delegate = &conversation.Automation{Kind: conversation.AutomationWorkflow, ID: "wf-9"}
		}},
		{"the agent runs cannot be read", "run_unreadable", func(h *continuationHarness) { h.runs.err = errors.New("db down") }},
		{"a newer agent run exists", "superseded", func(h *continuationHarness) { h.runs.latest.RunID = "run-2" }},
		{"the conversation was finished", "superseded", func(h *continuationHarness) { h.runs.latest = nil }},
		{"the window cannot be read", "window_unreadable", func(h *continuationHarness) { h.channels.windowErr = errors.New("api down") }},
		{"the window is closed", "window_closed", func(h *continuationHarness) { h.channels.open = false }},
		{"the lock store is down", "busy", func(h *continuationHarness) { h.lockState.err = errors.New("redis down") }},
		{"the history cannot be read", "history_unreadable", func(h *continuationHarness) { h.messages.err = errors.New("db down") }},
		{"the customer already wrote again", "customer_spoke", func(h *continuationHarness) { h.messages.spoke = true }},
		{"the claim cannot be recorded", "claim_failed", func(h *continuationHarness) { h.claims.err = errors.New("db down") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newContinuationHarness()
			tc.mutate(h)
			outcome := h.continuation(t).Handle(context.Background(), finished(workflow.RunStatusCompleted, true))
			if outcome != tc.outcome || len(h.turns()) != 0 {
				t.Fatalf("outcome = %q (want %q), turns = %d", outcome, tc.outcome, len(h.turns()))
			}
		})
	}
}

func TestAContinuationTheReplyPathSkippedIsCountedAsSkippedNotSent(t *testing.T) {
	for _, reason := range []string{replySkipInsufficientBalance, replySkipWindowClosed, "lead_opted_out"} {
		t.Run(reason, func(t *testing.T) {
			h := newContinuationHarness()
			h.channels.turnErr = skippedReply(reason)

			outcome := h.continuation(t).Handle(context.Background(), finished(workflow.RunStatusCompleted, true))

			if outcome != "skipped_"+reason || len(h.metrics.outcomes) != 1 || h.metrics.outcomes[0] != "skipped_"+reason {
				t.Fatalf("outcome = %q, metrics = %v; a reply that was never sent must not count as sent", outcome, h.metrics.outcomes)
			}
			if h.claims.released != 0 {
				t.Fatal("a final skip keeps its claim, so the sweep never retries it")
			}
		})
	}
}

func TestARetryableSkipAfterTheClaimHandsTheRunBackToTheSweep(t *testing.T) {
	for name, tc := range map[string]struct {
		err     error
		outcome string
	}{
		"paced":       {skippedReply(replySkipRateLimited), "retry_" + replySkipRateLimited},
		"not started": {errors.Join(conversation.ErrAgentTurnNotStarted, errors.New("conn reset")), "retry_not_started"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newContinuationHarness()
			h.channels.turnErr = tc.err
			c := h.continuation(t)
			ev := finished(workflow.RunStatusCompleted, true)

			if got := c.Handle(context.Background(), ev); got != tc.outcome || h.claims.released != 1 {
				t.Fatalf("outcome = %q released = %d", got, h.claims.released)
			}
			h.channels.turnErr = nil
			if got := c.Handle(context.Background(), ev); got != "sent" {
				t.Fatalf("the sweep's replay = %q, want the turn to go out", got)
			}
		})
	}
}

func TestRetriesStopAtTheBound(t *testing.T) {
	h := newContinuationHarness()
	h.channels.turnErr = skippedReply(replySkipRateLimited)
	c := h.continuation(t)
	ev := finished(workflow.RunStatusCompleted, true)

	var outcomes []string
	for i := 0; i < workflow.MaxAgentContinuationAttempts+2; i++ {
		outcomes = append(outcomes, c.Handle(context.Background(), ev))
	}
	retries := 0
	for _, o := range outcomes {
		if o == "retry_"+replySkipRateLimited {
			retries++
		}
	}
	if retries != workflow.MaxAgentContinuationAttempts-1 || outcomes[workflow.MaxAgentContinuationAttempts-1] != "skipped_"+replySkipRateLimited || outcomes[len(outcomes)-1] != "already_continued" {
		t.Fatalf("outcomes = %v; the last allowed attempt is final and later replays do nothing", outcomes)
	}
}

func TestAReleaseThatFailsIsReportedAndNeverCountedAsSent(t *testing.T) {
	h := newContinuationHarness()
	h.channels.turnErr = skippedReply(replySkipRateLimited)
	h.claims.releaseErr = errors.New("db down")

	if got := h.continuation(t).Handle(context.Background(), finished(workflow.RunStatusCompleted, true)); got != "release_failed" {
		t.Fatalf("outcome = %q", got)
	}
}

func TestARetriedEventContinuesOnlyOnce(t *testing.T) {
	h := newContinuationHarness()
	c := h.continuation(t)
	ev := finished(workflow.RunStatusCompleted, true)

	first := c.Handle(context.Background(), ev)
	second := c.Handle(context.Background(), ev)

	if first != "sent" || second != "already_continued" || len(h.turns()) != 1 {
		t.Fatalf("first = %q, second = %q, turns = %d", first, second, len(h.turns()))
	}
}

func TestTheContinuationWaitsForAnInboundTurnOnTheSameConversation(t *testing.T) {
	h := newContinuationHarness()
	c := h.continuation(t)
	inbound := NewAgentTurnLock(h.lockState)
	release, err := inbound.Acquire(context.Background(), "entry-1", shared.EntryTypeTelegram)
	if err != nil {
		t.Fatal(err)
	}

	ctx := untilSettled(t)
	done := make(chan string, 1)
	go func() { done <- c.Handle(ctx, finished(workflow.RunStatusCompleted, true)) }()

	h.lockState.awaitWaiter(t, ctx)
	select {
	case outcome := <-done:
		t.Fatalf("the continuation ran (%q) while an inbound turn held the conversation", outcome)
	default:
	}
	release()
	select {
	case outcome := <-done:
		if outcome != "sent" {
			t.Fatalf("outcome = %q", outcome)
		}
	case <-ctx.Done():
		t.Fatal("the continuation never ran after the inbound turn finished")
	}
}

func TestTheContinuationHoldsTheConversationWhileItRuns(t *testing.T) {
	h := newContinuationHarness()
	held := false
	h.channels.onTurn = func() {
		_, held = h.lockState.holder(agentTurnKey("entry-1", shared.EntryTypeTelegram))
	}

	h.continuation(t).Handle(context.Background(), finished(workflow.RunStatusCompleted, true))

	if !held {
		t.Fatal("an inbound turn could start while the continuation was replying")
	}
}

func TestAContinuationReplayedByTheSweepAfterTheOriginalWasDeliveredSendsNothing(t *testing.T) {
	h := newContinuationHarness()
	c := h.continuation(t)
	stored := &workflow.WorkflowRun{
		ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: string(shared.EntryTypeTelegram),
		StartedBy: workflow.StartedByAgent("agent-7"), Status: workflow.RunStatusCompleted,
		AgentResult: map[string]interface{}{"status": "matriculado"}, UpdatedAt: runFinishedAt,
	}
	original, ok := workflow.AgentRunFinishedOf(stored)
	if !ok {
		t.Fatal("the stored run owes the agent a turn")
	}
	if got := c.Handle(context.Background(), original); got != "sent" {
		t.Fatalf("original = %q", got)
	}

	replayed, _ := workflow.AgentRunFinishedOf(stored)
	if got := c.Handle(context.Background(), replayed); got != "already_continued" {
		t.Fatalf("replayed = %q, the durable claim must make the sweep idempotent", got)
	}
	if len(h.turns()) != 1 {
		t.Fatalf("turns = %d, want exactly one agent turn", len(h.turns()))
	}
}
