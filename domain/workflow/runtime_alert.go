package workflow

import (
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/tools"
)

const WorkflowNotActiveCode = "workflow_not_active"

const (
	AgentRunWorkflowTool = "run_workflow"
	AgentHTTPRequestTool = "http_request"
)

type RuntimeAlertKind string

const (
	AlertWorkflowCalledInactive RuntimeAlertKind = "workflow_called_inactive"
	AlertNodeFailing            RuntimeAlertKind = "node_failing"
	AlertWebhookRejecting       RuntimeAlertKind = "webhook_rejecting"
	AlertRunStuck               RuntimeAlertKind = "run_stuck"
	AlertAgentToolFailing       RuntimeAlertKind = "agent_tool_failing"
	AlertInboundLookupFailed    RuntimeAlertKind = "inbound_lookup_failed"
)

type RuntimeAlertStatus string

const (
	RuntimeAlertOpen     RuntimeAlertStatus = "open"
	RuntimeAlertResolved RuntimeAlertStatus = "resolved"
)

type RuntimeAlertSeverity string

const (
	RuntimeAlertHigh   RuntimeAlertSeverity = "high"
	RuntimeAlertMedium RuntimeAlertSeverity = "medium"
)

type AlertFixAction string

const (
	FixPublish          AlertFixAction = "publish"
	FixOpenRun          AlertFixAction = "open_run"
	FixOpenNode         AlertFixAction = "open_node"
	FixViewDeliveries   AlertFixAction = "view_deliveries"
	FixOpenAgent        AlertFixAction = "open_agent"
	FixOpenConversation AlertFixAction = "open_conversation"
)

const (
	RuntimeAlertEvaluationPeriod = 2 * time.Minute

	CalledInactiveWindow = time.Hour

	NodeFailingWindow           = 24 * time.Hour
	NodeFailingRecentExecutions = 10
	NodeFailingMinFailures      = 3
	NodeRecoverySuccesses       = 3

	WebhookRejectingWindow        = time.Hour
	WebhookRejectingMinRejections = 5

	AgentToolFailingWindow    = time.Hour
	AgentToolFailingMinErrors = 3

	QuietAlertResolveAfter    = time.Hour
	StuckRunAlertResolveAfter = 24 * time.Hour

	RuntimeAlertRenotifyAfter = time.Hour
	RuntimeAlertResolvedShown = 24 * time.Hour
	RuntimeAlertRetention     = 30 * 24 * time.Hour
)

type alertSignalSource int

const (
	signalFromEvents alertSignalSource = iota
	signalFromDeliveries
	signalFromToolCalls
)

type runtimeAlertRule struct {
	severity  RuntimeAlertSeverity
	fix       AlertFixAction
	minCount  int
	quietFor  time.Duration
	quietOn   alertSignalSource
	byCount   bool
	byRunning bool
}

var runtimeAlertRules = map[RuntimeAlertKind]runtimeAlertRule{
	AlertWorkflowCalledInactive: {severity: RuntimeAlertHigh, fix: FixPublish, minCount: 1, byRunning: true},
	AlertNodeFailing:            {severity: RuntimeAlertHigh, fix: FixOpenRun, minCount: NodeFailingMinFailures, byCount: true},
	AlertWebhookRejecting:       {severity: RuntimeAlertMedium, fix: FixViewDeliveries, minCount: WebhookRejectingMinRejections, quietFor: QuietAlertResolveAfter, quietOn: signalFromDeliveries},
	AlertRunStuck:               {severity: RuntimeAlertMedium, fix: FixOpenRun, minCount: 1, quietFor: StuckRunAlertResolveAfter},
	AlertAgentToolFailing:       {severity: RuntimeAlertMedium, fix: FixOpenAgent, minCount: AgentToolFailingMinErrors, quietFor: QuietAlertResolveAfter, quietOn: signalFromToolCalls},
	AlertInboundLookupFailed:    {severity: RuntimeAlertMedium, fix: FixOpenConversation, minCount: 1, quietFor: QuietAlertResolveAfter},
}

func (k RuntimeAlertKind) Valid() bool {
	_, ok := runtimeAlertRules[k]
	return ok
}

func (k RuntimeAlertKind) Severity() RuntimeAlertSeverity { return runtimeAlertRules[k].severity }

func (k RuntimeAlertKind) FixAction() AlertFixAction { return runtimeAlertRules[k].fix }

func (k RuntimeAlertKind) MinCount() int { return runtimeAlertRules[k].minCount }

type RuntimeAlertKey struct {
	WorkspaceID string
	WorkflowID  string
	NodeID      string
	AgentID     string
	Subject     string
	Kind        RuntimeAlertKind
}

type RuntimeAlert struct {
	ID         string
	Key        RuntimeAlertKey
	Severity   RuntimeAlertSeverity
	FixAction  AlertFixAction
	Count      int
	FirstSeen  time.Time
	LastSeen   time.Time
	Sample     string
	Status     RuntimeAlertStatus
	ResolvedAt *time.Time
	NotifiedAt *time.Time
}

func (a RuntimeAlert) EpisodeKey() string { return "workflow_alert:" + a.ID }

func (a RuntimeAlert) NotificationDue(now time.Time) bool {
	return a.Status == RuntimeAlertOpen && (a.NotifiedAt == nil || !now.Before(a.NotifiedAt.Add(RuntimeAlertRenotifyAfter)))
}

func (a RuntimeAlert) Described() RuntimeAlert {
	a.Severity = a.Key.Kind.Severity()
	a.FixAction = a.Key.Kind.FixAction()
	return a
}

type RuntimeAlertSignal struct {
	Key       RuntimeAlertKey
	Count     int
	FirstSeen time.Time
	LastSeen  time.Time
	Sample    string
}

func (s RuntimeAlertSignal) Valid() bool {
	return strings.TrimSpace(s.Key.WorkspaceID) != "" && s.Key.Kind.Valid() && s.Count > 0 && !s.LastSeen.IsZero()
}

type RuntimeAlertChanges struct {
	Opened  []RuntimeAlert
	Touched []RuntimeAlert
}

func ObserveRuntimeAlerts(open []RuntimeAlert, lastResolved map[RuntimeAlertKey]time.Time, signals []RuntimeAlertSignal) RuntimeAlertChanges {
	byKey := make(map[RuntimeAlertKey]RuntimeAlert, len(open))
	for _, a := range open {
		if a.Status == RuntimeAlertOpen {
			byKey[a.Key] = a
		}
	}
	var changes RuntimeAlertChanges
	for _, s := range mergeRuntimeAlertSignals(signals) {
		if current, isOpen := byKey[s.Key]; isOpen {
			if !s.LastSeen.After(current.LastSeen) {
				continue
			}
			current.Count, current.LastSeen, current.Sample = s.Count, s.LastSeen, s.Sample
			changes.Touched = append(changes.Touched, current)
			continue
		}
		if s.Count < s.Key.Kind.MinCount() {
			continue
		}
		if resolvedAt, ok := lastResolved[s.Key]; ok && !s.LastSeen.After(resolvedAt) {
			continue
		}
		changes.Opened = append(changes.Opened, OpenRuntimeAlert(s))
	}
	return changes
}

func OpenRuntimeAlert(s RuntimeAlertSignal) RuntimeAlert {
	return RuntimeAlert{
		Key:       s.Key,
		Count:     s.Count,
		FirstSeen: s.FirstSeen,
		LastSeen:  s.LastSeen,
		Sample:    s.Sample,
		Status:    RuntimeAlertOpen,
	}.Described()
}

func mergeRuntimeAlertSignals(signals []RuntimeAlertSignal) []RuntimeAlertSignal {
	order := make([]RuntimeAlertKey, 0, len(signals))
	merged := make(map[RuntimeAlertKey]RuntimeAlertSignal, len(signals))
	for _, s := range signals {
		if !s.Valid() {
			continue
		}
		current, seen := merged[s.Key]
		if !seen {
			order = append(order, s.Key)
			merged[s.Key] = s
			continue
		}
		current.Count += s.Count
		if !s.FirstSeen.IsZero() && (current.FirstSeen.IsZero() || s.FirstSeen.Before(current.FirstSeen)) {
			current.FirstSeen = s.FirstSeen
		}
		if s.LastSeen.After(current.LastSeen) {
			current.LastSeen, current.Sample = s.LastSeen, s.Sample
		}
		merged[s.Key] = current
	}
	out := make([]RuntimeAlertSignal, len(order))
	for i, k := range order {
		out[i] = merged[k]
	}
	return out
}

type RuntimeAlertFacts struct {
	WorkflowsRead  bool
	Workflows      map[string]*Workflow
	NodeStatsRead  bool
	NodeSuccesses  map[RuntimeAlertKey]int
	DeliveriesRead bool
	ToolCallsRead  bool
}

func (a RuntimeAlert) ResolvedBy(f RuntimeAlertFacts, now time.Time) bool {
	rule := runtimeAlertRules[a.Key.Kind]
	if a.Key.WorkflowID != "" && f.WorkflowsRead {
		w := f.Workflows[a.Key.WorkflowID]
		if w == nil || w.WorkspaceID != a.Key.WorkspaceID {
			return true
		}
		if rule.byRunning && w.Callable() {
			return true
		}
	}
	if rule.byCount && f.NodeStatsRead && f.NodeSuccesses[a.Key] >= NodeRecoverySuccesses {
		return true
	}
	return rule.quietFor > 0 && f.sourceRead(rule.quietOn) && !now.Before(a.LastSeen.Add(rule.quietFor))
}

func (f RuntimeAlertFacts) sourceRead(source alertSignalSource) bool {
	switch source {
	case signalFromDeliveries:
		return f.DeliveriesRead
	case signalFromToolCalls:
		return f.ToolCallsRead
	default:
		return true
	}
}

type RejectedDeliveryGroup struct {
	WorkspaceID string
	WorkflowID  string
	Reason      string
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	Sample      string
}

func SignalsOfRejectedDeliveries(groups []RejectedDeliveryGroup) []RuntimeAlertSignal {
	out := make([]RuntimeAlertSignal, 0, len(groups))
	for _, g := range groups {
		key := RuntimeAlertKey{WorkspaceID: g.WorkspaceID, WorkflowID: g.WorkflowID, Kind: AlertWorkflowCalledInactive}
		if g.Reason != WorkflowNotActiveCode {
			key.Kind, key.Subject = AlertWebhookRejecting, g.Reason
		}
		out = append(out, RuntimeAlertSignal{Key: key, Count: g.Count, FirstSeen: g.FirstSeen, LastSeen: g.LastSeen, Sample: g.Sample})
	}
	return out
}

type FailedToolCallGroup struct {
	WorkspaceID string
	AgentID     string
	ToolName    string
	ErrorCode   string
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	Sample      string
}

func SignalsOfFailedToolCalls(groups []FailedToolCallGroup) []RuntimeAlertSignal {
	out := make([]RuntimeAlertSignal, 0, len(groups))
	for _, g := range groups {
		workflowID, boundToWorkflow := tools.BoundKeyOf(g.ToolName, AgentRunWorkflowTool)
		if !boundToWorkflow && strings.ToLower(g.ToolName) != AgentHTTPRequestTool {
			continue
		}
		key := RuntimeAlertKey{WorkspaceID: g.WorkspaceID, WorkflowID: workflowID, AgentID: g.AgentID, Subject: g.ToolName, Kind: AlertAgentToolFailing}
		if g.ErrorCode == WorkflowNotActiveCode {
			if !boundToWorkflow {
				continue
			}
			key = RuntimeAlertKey{WorkspaceID: g.WorkspaceID, WorkflowID: workflowID, Kind: AlertWorkflowCalledInactive}
		}
		out = append(out, RuntimeAlertSignal{Key: key, Count: g.Count, FirstSeen: g.FirstSeen, LastSeen: g.LastSeen, Sample: g.Sample})
	}
	return out
}

func StuckRunSignal(run *WorkflowRun, at time.Time) RuntimeAlertSignal {
	if run == nil {
		return RuntimeAlertSignal{}
	}
	return RuntimeAlertSignal{
		Key:       RuntimeAlertKey{WorkspaceID: run.WorkspaceID, WorkflowID: run.WorkflowID, Kind: AlertRunStuck},
		Count:     1,
		FirstSeen: at,
		LastSeen:  at,
		Sample:    run.ID,
	}
}

func InboundLookupFailedSignal(event TriggerEvent, at time.Time) RuntimeAlertSignal {
	return RuntimeAlertSignal{
		Key:       RuntimeAlertKey{WorkspaceID: event.WorkspaceID, Subject: event.EntryType, Kind: AlertInboundLookupFailed},
		Count:     1,
		FirstSeen: at,
		LastSeen:  at,
		Sample:    event.EntryID,
	}
}

type RuntimeAlertRecorder interface {
	RecordRuntimeAlert(signal RuntimeAlertSignal) error
}

type RuntimeAlertRepository interface {
	RuntimeAlertRecorder
	Tracked(resolvedSince time.Time, limit int) ([]RuntimeAlert, error)
	OpenAlerts(alerts []RuntimeAlert) error
	TouchAlerts(alerts []RuntimeAlert) error
	ResolveAlerts(ids []string, at time.Time) error
	MarkNotified(ids []string, at time.Time) error
	ListForWorkflow(workspaceID, workflowID string, visibility conversation.VisibilityScope, resolvedSince time.Time) ([]RuntimeAlert, error)
	ListOpen(workspaceID string, departments DepartmentRestriction, visibility conversation.VisibilityScope, pagination shared.Pagination) (*shared.PaginatedResult[*RuntimeAlert], error)
	DeleteResolvedBefore(cutoff time.Time, limit int) (int64, error)
}

type NodeAlertProbe struct {
	WorkflowID string
	NodeID     string
}

type RuntimeAlertSignals interface {
	RejectedDeliveries(since time.Time) ([]RejectedDeliveryGroup, error)
	FailedToolCalls(since time.Time) ([]FailedToolCallGroup, error)
	FailingNodes(since time.Time, lastExecutions int) ([]RuntimeAlertSignal, error)
	NodeSuccessesAfterLastFailure(probes []NodeAlertProbe, since time.Time) (map[NodeAlertProbe]int, error)
}

func (a RuntimeAlert) RunID() string {
	if a.Key.Kind == AlertNodeFailing || a.Key.Kind == AlertRunStuck {
		return a.Sample
	}
	return ""
}

func (a RuntimeAlert) Conversation() (shared.EntryRef, bool) {
	if a.Key.Kind != AlertInboundLookupFailed || a.Sample == "" {
		return shared.EntryRef{}, false
	}
	return shared.EntryRef{EntryID: a.Sample, EntryType: shared.EntryType(a.Key.Subject)}, true
}
