package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const trackedRuntimeAlertsLimit = 5000

type WorkflowsByID interface {
	FindByIDs(ids []string) ([]*workflow.Workflow, error)
}

type AgentsByID interface {
	FindByIDs(ids []string) ([]*agent.Agent, error)
}

type RuntimeAlertsDeps struct {
	Alerts     workflow.RuntimeAlertRepository
	Signals    workflow.RuntimeAlertSignals
	Workflows  WorkflowsByID
	Agents     AgentsByID
	Notices    notification.EpisodePublisher
	Steps      StepNamer
	Visibility conversation.Visibility
}

type RuntimeAlerts struct {
	alerts     workflow.RuntimeAlertRepository
	signals    workflow.RuntimeAlertSignals
	workflows  WorkflowsByID
	agents     AgentsByID
	notices    notification.EpisodePublisher
	steps      StepNamer
	visibility conversation.Visibility
	now        func() time.Time
}

func NewRuntimeAlerts(d RuntimeAlertsDeps) (*RuntimeAlerts, error) {
	if d.Alerts == nil || d.Signals == nil || d.Workflows == nil || d.Agents == nil || d.Notices == nil || d.Steps == nil || d.Visibility == nil {
		return nil, errors.New("workflow runtime alerts: the alert store, signals, workflows, agents, notifications, step names and conversation visibility are required")
	}
	return &RuntimeAlerts{
		alerts: d.Alerts, signals: d.Signals, workflows: d.Workflows, agents: d.Agents, notices: d.Notices, steps: d.Steps, visibility: d.Visibility,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (a *RuntimeAlerts) Execute(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := a.now()
	tracked, err := a.alerts.Tracked(now.Add(-workflow.RuntimeAlertResolvedShown), trackedRuntimeAlertsLimit)
	if err != nil {
		return fmt.Errorf("read the tracked runtime alerts: %w", err)
	}
	open, lastResolved := splitTracked(tracked)
	collected, signalErr := a.collectSignals(now)
	changes := workflow.ObserveRuntimeAlerts(open, lastResolved, collected.signals)
	if err := a.alerts.TouchAlerts(changes.Touched); err != nil {
		return errors.Join(signalErr, fmt.Errorf("touch runtime alerts: %w", err))
	}
	if err := a.alerts.OpenAlerts(changes.Opened); err != nil {
		return errors.Join(signalErr, fmt.Errorf("open runtime alerts: %w", err))
	}
	if len(changes.Touched)+len(changes.Opened) > 0 {
		if tracked, err = a.alerts.Tracked(now.Add(-workflow.RuntimeAlertResolvedShown), trackedRuntimeAlertsLimit); err != nil {
			return errors.Join(signalErr, fmt.Errorf("read the tracked runtime alerts: %w", err))
		}
		open, _ = splitTracked(tracked)
	}
	facts := a.facts(open)
	facts.DeliveriesRead, facts.ToolCallsRead = collected.deliveriesRead, collected.toolCallsRead
	stillOpen, resolveErr := a.resolve(ctx, open, facts, now)
	return errors.Join(signalErr, resolveErr, a.notify(ctx, stillOpen, facts, now))
}

func splitTracked(tracked []workflow.RuntimeAlert) ([]workflow.RuntimeAlert, map[workflow.RuntimeAlertKey]time.Time) {
	open := make([]workflow.RuntimeAlert, 0, len(tracked))
	lastResolved := map[workflow.RuntimeAlertKey]time.Time{}
	for _, alert := range tracked {
		if alert.Status == workflow.RuntimeAlertOpen {
			open = append(open, alert)
			continue
		}
		if alert.ResolvedAt != nil && alert.ResolvedAt.After(lastResolved[alert.Key]) {
			lastResolved[alert.Key] = *alert.ResolvedAt
		}
	}
	return open, lastResolved
}

type collectedSignals struct {
	signals        []workflow.RuntimeAlertSignal
	deliveriesRead bool
	toolCallsRead  bool
}

func (a *RuntimeAlerts) collectSignals(now time.Time) (collectedSignals, error) {
	var out collectedSignals
	var errs []error
	deliveries, err := a.signals.RejectedDeliveries(now.Add(-max(workflow.CalledInactiveWindow, workflow.WebhookRejectingWindow)))
	if err != nil {
		errs = append(errs, fmt.Errorf("rejected webhook deliveries: %w", err))
	}
	out.deliveriesRead = err == nil
	out.signals = append(out.signals, workflow.SignalsOfRejectedDeliveries(deliveries)...)
	calls, err := a.signals.FailedToolCalls(now.Add(-max(workflow.CalledInactiveWindow, workflow.AgentToolFailingWindow)))
	if err != nil {
		errs = append(errs, fmt.Errorf("failed agent tool calls: %w", err))
	}
	out.toolCallsRead = err == nil
	out.signals = append(out.signals, workflow.SignalsOfFailedToolCalls(calls)...)
	nodes, err := a.signals.FailingNodes(now.Add(-workflow.NodeFailingWindow), workflow.NodeFailingRecentExecutions)
	if err != nil {
		errs = append(errs, fmt.Errorf("failing nodes: %w", err))
	}
	out.signals = append(out.signals, nodes...)
	for _, e := range errs {
		slog.Error("workflow.alerts.signal_unreadable", slog.String("error", e.Error()))
	}
	return out, errors.Join(errs...)
}

type alertNames struct {
	workflows map[string]*workflow.Workflow
	agents    map[string]*agent.Agent
}

func (a *RuntimeAlerts) facts(open []workflow.RuntimeAlert) workflow.RuntimeAlertFacts {
	facts := workflow.RuntimeAlertFacts{Workflows: map[string]*workflow.Workflow{}}
	workflowIDs := uniqueAlertIDs(open, func(alert workflow.RuntimeAlert) string { return alert.Key.WorkflowID })
	if found, err := a.workflows.FindByIDs(workflowIDs); err != nil {
		slog.Error("workflow.alerts.workflows_unreadable", slog.String("error", err.Error()))
	} else {
		facts.WorkflowsRead = true
		for _, w := range found {
			if w != nil {
				facts.Workflows[w.ID] = w
			}
		}
	}

	var probes []workflow.NodeAlertProbe
	var since time.Time
	for _, alert := range open {
		if alert.Key.Kind != workflow.AlertNodeFailing {
			continue
		}
		probes = append(probes, workflow.NodeAlertProbe{WorkflowID: alert.Key.WorkflowID, NodeID: alert.Key.NodeID})
		if since.IsZero() || alert.LastSeen.Before(since) {
			since = alert.LastSeen
		}
	}
	if len(probes) == 0 {
		return facts
	}
	successes, err := a.signals.NodeSuccessesAfterLastFailure(probes, since)
	if err != nil {
		slog.Error("workflow.alerts.node_stats_unreadable", slog.String("error", err.Error()))
		return facts
	}
	facts.NodeStatsRead = true
	facts.NodeSuccesses = map[workflow.RuntimeAlertKey]int{}
	for _, alert := range open {
		if alert.Key.Kind == workflow.AlertNodeFailing {
			facts.NodeSuccesses[alert.Key] = successes[workflow.NodeAlertProbe{WorkflowID: alert.Key.WorkflowID, NodeID: alert.Key.NodeID}]
		}
	}
	return facts
}

func (a *RuntimeAlerts) resolve(ctx context.Context, open []workflow.RuntimeAlert, facts workflow.RuntimeAlertFacts, now time.Time) ([]workflow.RuntimeAlert, error) {
	stillOpen := make([]workflow.RuntimeAlert, 0, len(open))
	var ids []string
	keysByWorkspace := map[string][]string{}
	for _, alert := range open {
		if !alert.ResolvedBy(facts, now) {
			stillOpen = append(stillOpen, alert)
			continue
		}
		ids = append(ids, alert.ID)
		keysByWorkspace[alert.Key.WorkspaceID] = append(keysByWorkspace[alert.Key.WorkspaceID], alert.EpisodeKey())
	}
	if len(ids) == 0 {
		return stillOpen, nil
	}
	if err := a.alerts.ResolveAlerts(ids, now); err != nil {
		return stillOpen, fmt.Errorf("resolve runtime alerts: %w", err)
	}
	var errs []error
	for workspaceID, keys := range keysByWorkspace {
		if err := a.notices.Resolve(ctx, workspaceID, keys...); err != nil {
			slog.Error("workflow.alerts.episode_resolve_failed", slog.String("workspace_id", workspaceID), slog.String("error", err.Error()))
			errs = append(errs, err)
		}
	}
	return stillOpen, errors.Join(errs...)
}

func (a *RuntimeAlerts) notify(ctx context.Context, open []workflow.RuntimeAlert, facts workflow.RuntimeAlertFacts, now time.Time) error {
	due := make([]workflow.RuntimeAlert, 0, len(open))
	for _, alert := range open {
		if alert.NotificationDue(now) {
			due = append(due, alert)
		}
	}
	if len(due) == 0 {
		return nil
	}
	names := alertNames{workflows: facts.Workflows, agents: a.agentNames(due)}
	var notified []string
	var errs []error
	for _, alert := range due {
		draft := runtimeAlertDraft(alert, names, a.steps)
		if _, err := a.notices.Publish(ctx, draft); err != nil {
			slog.Error("workflow.alerts.publish_failed", slog.String("workspace_id", alert.Key.WorkspaceID), slog.String("workflow_id", alert.Key.WorkflowID), slog.String("alert_id", alert.ID), slog.String("error", err.Error()))
			errs = append(errs, err)
			continue
		}
		notified = append(notified, alert.ID)
	}
	if err := a.alerts.MarkNotified(notified, now); err != nil {
		errs = append(errs, fmt.Errorf("mark runtime alerts notified: %w", err))
	}
	return errors.Join(errs...)
}

func (a *RuntimeAlerts) agentNames(alerts []workflow.RuntimeAlert) map[string]*agent.Agent {
	out := map[string]*agent.Agent{}
	ids := uniqueAlertIDs(alerts, func(alert workflow.RuntimeAlert) string { return alert.Key.AgentID })
	if len(ids) == 0 {
		return out
	}
	found, err := a.agents.FindByIDs(ids)
	if err != nil {
		slog.Warn("workflow.alerts.agents_unreadable", slog.String("error", err.Error()))
		return out
	}
	for _, ag := range found {
		if ag != nil {
			out[ag.ID] = ag
		}
	}
	return out
}

func uniqueAlertIDs(alerts []workflow.RuntimeAlert, of func(workflow.RuntimeAlert) string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, alert := range alerts {
		id := of(alert)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (a *RuntimeAlerts) ForWorkflow(viewer shared.Person, workspaceID, workflowID string) ([]workflow.RuntimeAlert, error) {
	if workspaceID == "" || workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	return a.alerts.ListForWorkflow(workspaceID, workflowID, conversationScope(a.visibility, viewer, workspaceID), a.now().Add(-workflow.RuntimeAlertResolvedShown))
}

func (a *RuntimeAlerts) OpenInWorkspace(viewer shared.Person, scope workflow.Scope, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.RuntimeAlert], error) {
	if scope.WorkspaceID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	return a.alerts.ListOpen(scope.WorkspaceID, workflow.DepartmentRestrictionOf(scope.Departments), conversationScope(a.visibility, viewer, scope.WorkspaceID), pagination)
}
