package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/metrics"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type AgentContinuationTurn struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType
	AgentID     string
	Note        string
}

type AgentTurnContinuer interface {
	ContinuationWindowOpen(ctx context.Context, turn AgentContinuationTurn) (bool, error)
	ContinueAgentTurn(ctx context.Context, turn AgentContinuationTurn) error
}

type AgentTurnContinuers struct {
	Official AgentTurnContinuer
	Channels AgentTurnContinuer
}

var errNoContinuer = errors.New("agent continuation: no reply path for this channel")

func (c AgentTurnContinuers) For(entryType shared.EntryType) (AgentTurnContinuer, error) {
	continuer := c.Channels
	if entryType == shared.EntryTypeWhatsApp {
		continuer = c.Official
	}
	if continuer == nil {
		return nil, fmt.Errorf("%w: %s", errNoContinuer, entryType)
	}
	return continuer, nil
}

type AgentContinuationDeps struct {
	Automations conversation.EntryAutomationReader
	Runs        workflow.AgentCallRuns
	Claims      workflow.AgentContinuationClaims
	Messages    conversation.CustomerSpeech
	Continuers  AgentTurnContinuers
	Lock        *AgentTurnLock
	Metrics     metrics.WorkflowMetricsRecorder
}

type AgentContinuation struct {
	d AgentContinuationDeps
}

func NewAgentContinuation(d AgentContinuationDeps) (*AgentContinuation, error) {
	missing := []string{}
	for name, dep := range map[string]any{
		"automation reader":  d.Automations,
		"agent runs":         d.Runs,
		"continuation claim": d.Claims,
		"messages":           d.Messages,
		"turn lock":          d.Lock,
		"metrics":            d.Metrics,
	} {
		if dep == nil {
			missing = append(missing, name)
		}
	}
	if d.Continuers.Official == nil || d.Continuers.Channels == nil {
		missing = append(missing, "channel reply paths")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("agent continuation: missing %s", strings.Join(missing, ", "))
	}
	return &AgentContinuation{d: d}, nil
}

func (c *AgentContinuation) Handle(ctx context.Context, ev workflow.AgentRunFinished) string {
	outcome, err := c.handle(ctx, ev)
	attrs := []any{
		slog.String("run_id", ev.RunID), slog.String("workspace_id", ev.WorkspaceID),
		slog.String("entry_id", ev.EntryID), slog.String("entry_type", ev.EntryType),
		slog.String("agent_id", ev.AgentID), slog.String("outcome", outcome),
	}
	switch {
	case err != nil:
		slog.Warn("conversation.agent_continuation.skipped", append(attrs, slog.String("error", err.Error()))...)
	case outcome == "sent":
		slog.Info("conversation.agent_continuation.sent", attrs...)
	default:
		slog.Info("conversation.agent_continuation.skipped", attrs...)
	}
	c.d.Metrics.IncWorkflowAgentContinuation(outcome)
	return outcome
}

func (c *AgentContinuation) handle(ctx context.Context, ev workflow.AgentRunFinished) (string, error) {
	kind := ev.Continuation()
	if kind == workflow.ContinuationNone {
		return "not_due", nil
	}

	profile, err := c.d.Automations.EntryAutomation(ev.EntryID, ev.EntryType)
	if err != nil {
		return "automation_unreadable", err
	}
	governing, governed := profile.Governing()
	if !governed || governing.Kind != conversation.AutomationAgent || governing.ID != ev.AgentID {
		return "automation_changed", nil
	}

	latest, err := c.d.Runs.LatestForEntry(ev.WorkspaceID, ev.EntryID, ev.EntryType)
	if err != nil {
		return "run_unreadable", err
	}
	if latest == nil || latest.RunID != ev.RunID {
		return "superseded", nil
	}

	turn := AgentContinuationTurn{
		WorkspaceID: ev.WorkspaceID,
		EntryID:     ev.EntryID,
		EntryType:   shared.EntryType(ev.EntryType),
		AgentID:     ev.AgentID,
		Note:        workflow.ContinuationNote(kind, latest.WorkflowName, workflow.HumanRunFailure(latest.Error)),
	}
	continuer, err := c.d.Continuers.For(turn.EntryType)
	if err != nil {
		return "no_channel", err
	}
	open, err := continuer.ContinuationWindowOpen(ctx, turn)
	if err != nil {
		return "window_unreadable", err
	}
	if !open {
		return "window_closed", nil
	}

	release, err := c.d.Lock.Acquire(ctx, turn.EntryID, turn.EntryType)
	if err != nil {
		return "busy", err
	}
	defer release()

	spoke, err := c.d.Messages.CustomerSpokeAfter(turn.EntryID, turn.EntryType, ev.FinishedAt)
	if err != nil {
		return "history_unreadable", err
	}
	if spoke {
		return "customer_spoke", nil
	}

	claimed, err := c.d.Claims.ClaimAgentContinuation(ev.RunID)
	if err != nil {
		return "claim_failed", err
	}
	if !claimed {
		return "already_continued", nil
	}

	err = continuer.ContinueAgentTurn(ctx, turn)
	reason, skipped := replySkipReason(err)
	if conversation.TurnRetryable(err) {
		released, releaseErr := c.d.Claims.ReleaseAgentContinuation(ev.RunID)
		if releaseErr != nil {
			return "release_failed", errors.Join(err, releaseErr)
		}
		if released {
			if skipped {
				return "retry_" + reason, nil
			}
			return "retry_not_started", err
		}
	}
	if skipped {
		return "skipped_" + reason, nil
	}
	if err != nil {
		return "failed", err
	}
	return "sent", nil
}
