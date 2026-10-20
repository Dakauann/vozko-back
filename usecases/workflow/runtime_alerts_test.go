package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

type memAlerts struct {
	rows         []workflow.RuntimeAlert
	next         int
	trackErr     error
	listedFor    []string
	scopes       []conversation.VisibilityScope
	restrictions []workflow.DepartmentRestriction
}

func (m *memAlerts) openIndex(key workflow.RuntimeAlertKey) int {
	for i, a := range m.rows {
		if a.Status == workflow.RuntimeAlertOpen && a.Key == key {
			return i
		}
	}
	return -1
}

func (m *memAlerts) RecordRuntimeAlert(s workflow.RuntimeAlertSignal) error {
	if !s.Valid() {
		return nil
	}
	if i := m.openIndex(s.Key); i >= 0 {
		m.rows[i].Count += s.Count
		if s.LastSeen.After(m.rows[i].LastSeen) {
			m.rows[i].LastSeen = s.LastSeen
		}
		m.rows[i].Sample = s.Sample
		return nil
	}
	return m.OpenAlerts([]workflow.RuntimeAlert{workflow.OpenRuntimeAlert(s)})
}

func (m *memAlerts) Tracked(resolvedSince time.Time, _ int) ([]workflow.RuntimeAlert, error) {
	if m.trackErr != nil {
		return nil, m.trackErr
	}
	out := []workflow.RuntimeAlert{}
	for _, a := range m.rows {
		if a.Status == workflow.RuntimeAlertOpen || (a.ResolvedAt != nil && !a.ResolvedAt.Before(resolvedSince)) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memAlerts) OpenAlerts(alerts []workflow.RuntimeAlert) error {
	for _, a := range alerts {
		if m.openIndex(a.Key) >= 0 {
			continue
		}
		m.next++
		a.ID = fmt.Sprintf("alert-%d", m.next)
		m.rows = append(m.rows, a.Described())
	}
	return nil
}

func (m *memAlerts) TouchAlerts(alerts []workflow.RuntimeAlert) error {
	for _, a := range alerts {
		for i := range m.rows {
			if m.rows[i].ID == a.ID && m.rows[i].Status == workflow.RuntimeAlertOpen {
				m.rows[i].Count, m.rows[i].LastSeen, m.rows[i].Sample = a.Count, a.LastSeen, a.Sample
			}
		}
	}
	return nil
}

func (m *memAlerts) ResolveAlerts(ids []string, at time.Time) error {
	for _, id := range ids {
		for i := range m.rows {
			if m.rows[i].ID == id && m.rows[i].Status == workflow.RuntimeAlertOpen {
				resolvedAt := at
				m.rows[i].Status, m.rows[i].ResolvedAt = workflow.RuntimeAlertResolved, &resolvedAt
			}
		}
	}
	return nil
}

func (m *memAlerts) MarkNotified(ids []string, at time.Time) error {
	for _, id := range ids {
		for i := range m.rows {
			if m.rows[i].ID == id {
				notifiedAt := at
				m.rows[i].NotifiedAt = &notifiedAt
			}
		}
	}
	return nil
}

func (m *memAlerts) ListForWorkflow(workspaceID, workflowID string, visibility conversation.VisibilityScope, resolvedSince time.Time) ([]workflow.RuntimeAlert, error) {
	m.listedFor = append(m.listedFor, workspaceID+"/"+workflowID)
	m.scopes = append(m.scopes, visibility)
	out := []workflow.RuntimeAlert{}
	tracked, _ := m.Tracked(resolvedSince, 0)
	for _, a := range tracked {
		if a.Key.WorkspaceID == workspaceID && a.Key.WorkflowID == workflowID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memAlerts) ListOpen(workspaceID string, departments workflow.DepartmentRestriction, visibility conversation.VisibilityScope, p shared.Pagination) (*shared.PaginatedResult[*workflow.RuntimeAlert], error) {
	m.scopes = append(m.scopes, visibility)
	m.restrictions = append(m.restrictions, departments)
	items := []*workflow.RuntimeAlert{}
	for i := range m.rows {
		if m.rows[i].Key.WorkspaceID == workspaceID && m.rows[i].Status == workflow.RuntimeAlertOpen {
			items = append(items, &m.rows[i])
		}
	}
	return shared.NewPaginatedResult(items, p, int64(len(items))), nil
}

func (m *memAlerts) DeleteResolvedBefore(time.Time, int) (int64, error) { return 0, nil }

func (m *memAlerts) only(t *testing.T) workflow.RuntimeAlert {
	t.Helper()
	if len(m.rows) != 1 {
		t.Fatalf("alerts = %+v, want one", m.rows)
	}
	return m.rows[0]
}

type scriptedSignals struct {
	deliveries    []workflow.RejectedDeliveryGroup
	deliveriesErr error
	toolCalls     []workflow.FailedToolCallGroup
	toolCallsErr  error
	nodes         []workflow.RuntimeAlertSignal
	successes     map[workflow.NodeAlertProbe]int
	successesErr  error
	windows       map[string]time.Time
}

func (s *scriptedSignals) seen(name string, since time.Time) {
	if s.windows == nil {
		s.windows = map[string]time.Time{}
	}
	s.windows[name] = since
}

func (s *scriptedSignals) RejectedDeliveries(since time.Time) ([]workflow.RejectedDeliveryGroup, error) {
	s.seen("deliveries", since)
	return s.deliveries, s.deliveriesErr
}

func (s *scriptedSignals) FailedToolCalls(since time.Time) ([]workflow.FailedToolCallGroup, error) {
	s.seen("tool_calls", since)
	return s.toolCalls, s.toolCallsErr
}

func (s *scriptedSignals) FailingNodes(since time.Time, lastExecutions int) ([]workflow.RuntimeAlertSignal, error) {
	s.seen("nodes", since)
	if lastExecutions != workflow.NodeFailingRecentExecutions {
		return nil, errors.New("the node window must look at the last 10 executions")
	}
	return s.nodes, nil
}

func (s *scriptedSignals) NodeSuccessesAfterLastFailure(probes []workflow.NodeAlertProbe, since time.Time) (map[workflow.NodeAlertProbe]int, error) {
	return s.successes, s.successesErr
}

type workflowsByIDStub map[string]*workflow.Workflow

func (w workflowsByIDStub) FindByIDs(ids []string) ([]*workflow.Workflow, error) {
	out := []*workflow.Workflow{}
	for _, id := range ids {
		if found, ok := w[id]; ok {
			out = append(out, found)
		}
	}
	return out, nil
}

type agentsByIDStub map[string]*agent.Agent

func (a agentsByIDStub) FindByIDs(ids []string) ([]*agent.Agent, error) {
	out := []*agent.Agent{}
	for _, id := range ids {
		if found, ok := a[id]; ok {
			out = append(out, found)
		}
	}
	return out, nil
}

type alertStepNames struct{}

func (alertStepNames) StepName(t workflow.NodeType) string {
	if t == workflow.NodeTypeActionHTTPRequest {
		return "Requisição HTTP"
	}
	return string(t)
}

type alertHarness struct {
	alerts    *memAlerts
	signals   *scriptedSignals
	workflows workflowsByIDStub
	notices   *notificationtest.Publisher
	uc        *RuntimeAlerts
	now       time.Time
}

func newAlertHarness(t *testing.T) *alertHarness {
	t.Helper()
	h := &alertHarness{
		alerts:  &memAlerts{},
		signals: &scriptedSignals{},
		workflows: workflowsByIDStub{"wf-1": {
			ID: "wf-1", WorkspaceID: "ws-1", Name: "Matrícula", Status: workflow.WorkflowStatusDraft,
			Graph: workflow.Graph{Nodes: []workflow.Node{{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest}}},
		}},
		notices: &notificationtest.Publisher{},
		now:     time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
	uc, err := NewRuntimeAlerts(RuntimeAlertsDeps{
		Alerts:     h.alerts,
		Signals:    h.signals,
		Workflows:  h.workflows,
		Agents:     agentsByIDStub{"ag-1": {ID: "ag-1", Name: "Bia"}},
		Notices:    h.notices,
		Steps:      alertStepNames{},
		Visibility: seesEverything{},
	})
	if err != nil {
		t.Fatal(err)
	}
	uc.now = func() time.Time { return h.now }
	h.uc = uc
	return h
}

func (h *alertHarness) evaluate(t *testing.T) {
	t.Helper()
	if err := h.uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func failingNode(count int, at time.Time) workflow.RuntimeAlertSignal {
	return workflow.RuntimeAlertSignal{
		Key:   workflow.RuntimeAlertKey{WorkspaceID: "ws-1", WorkflowID: "wf-1", NodeID: "n55", Kind: workflow.AlertNodeFailing},
		Count: count, FirstSeen: at.Add(-time.Hour), LastSeen: at, Sample: "run-7",
	}
}

func TestANodeFailingInThreeOfItsLastRunsOpensAHighAlertForWorkflowEditors(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.nodes = []workflow.RuntimeAlertSignal{failingNode(3, h.now)}
	h.evaluate(t)

	alert := h.alerts.only(t)
	if alert.Status != workflow.RuntimeAlertOpen || alert.NotifiedAt == nil {
		t.Fatalf("alert = %+v", alert)
	}
	d := h.notices.Only(t)
	if d.Category != notification.CategoryAutomations || d.Severity != notification.SeverityCritical || !d.Email {
		t.Fatalf("draft = %+v", d)
	}
	if !d.Audience.Equal(notification.Holding(workspace.ResourceWorkflows, workspace.ActionUpdate)) {
		t.Fatalf("audience = %+v", d.Audience)
	}
	if d.EpisodeKey != "workflow_alert:"+alert.ID || d.WorkspaceID != "ws-1" || d.Source != notification.SourceWorkflowAlert {
		t.Fatalf("episode = %q, workspace = %q", d.EpisodeKey, d.WorkspaceID)
	}
	if d.ActionURL != "/dashboard/workflows/wf-1?run=run-7" || d.ActionLabel != "Abrir execução" {
		t.Fatalf("action = %q %q", d.ActionURL, d.ActionLabel)
	}
	for _, fragment := range []string{"Matrícula", "n55", "Requisição HTTP", "3 das últimas 10"} {
		if !strings.Contains(d.Title+" "+d.Body, fragment) {
			t.Errorf("copy misses %q: %q / %q", fragment, d.Title, d.Body)
		}
	}
	if h.signals.windows["nodes"] != h.now.Add(-24*time.Hour) || h.signals.windows["deliveries"] != h.now.Add(-time.Hour) || h.signals.windows["tool_calls"] != h.now.Add(-time.Hour) {
		t.Fatalf("windows = %+v", h.signals.windows)
	}
}

func TestAnOpenAlertIsRepublishedHourlySoTheEpisodeCanSendItsReminder(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.nodes = []workflow.RuntimeAlertSignal{failingNode(3, h.now)}
	h.evaluate(t)
	h.now = h.now.Add(workflow.RuntimeAlertEvaluationPeriod)
	h.evaluate(t)
	if got := len(h.notices.Drafts()); got != 1 {
		t.Fatalf("drafts after two minutes = %d, want 1", got)
	}
	h.now = h.now.Add(workflow.RuntimeAlertRenotifyAfter)
	h.evaluate(t)
	if got := len(h.notices.Drafts()); got != 2 {
		t.Fatalf("drafts after an hour = %d, want 2 (the publisher decides which episode step goes out)", got)
	}
}

func TestACalledInactiveWorkflowAlertResolvesOncePublishedAndClosesItsEpisode(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.deliveries = []workflow.RejectedDeliveryGroup{{WorkspaceID: "ws-1", WorkflowID: "wf-1", Reason: workflow.WorkflowNotActiveCode, Count: 2, FirstSeen: h.now, LastSeen: h.now, Sample: "d-1"}}
	h.signals.toolCalls = []workflow.FailedToolCallGroup{{WorkspaceID: "ws-1", AgentID: "ag-1", ToolName: "run_workflow_wf-1", ErrorCode: workflow.WorkflowNotActiveCode, Count: 1, FirstSeen: h.now, LastSeen: h.now, Sample: "c-1"}}
	h.evaluate(t)

	alert := h.alerts.only(t)
	if alert.Key.Kind != workflow.AlertWorkflowCalledInactive || alert.Count != 3 {
		t.Fatalf("webhook and agent refusals are one condition: %+v", alert)
	}
	d := h.notices.Only(t)
	if d.Severity != notification.SeverityCritical || d.ActionURL != "/dashboard/workflows/wf-1?alert="+alert.ID || d.ActionLabel != "Publicar fluxo" || !strings.Contains(d.Body, "nunca foi publicado") {
		t.Fatalf("draft = %+v", d)
	}

	one := 1
	h.workflows["wf-1"].Status, h.workflows["wf-1"].PublishedVersion = workflow.WorkflowStatusActive, &one
	h.now = h.now.Add(workflow.RuntimeAlertEvaluationPeriod)
	h.evaluate(t)
	if got := h.alerts.only(t); got.Status != workflow.RuntimeAlertResolved {
		t.Fatalf("alert = %+v", got)
	}
	if !h.notices.ResolvedKeys()[alert.EpisodeKey()] {
		t.Fatalf("resolutions = %+v", h.notices.Resolutions())
	}
	h.now = h.now.Add(workflow.RuntimeAlertEvaluationPeriod)
	h.evaluate(t)
	if len(h.alerts.rows) != 1 {
		t.Fatalf("the refusals counted before publishing reopened the alert: %+v", h.alerts.rows)
	}
}

func TestANodeAlertResolvesAfterThreeSuccessesAndOnlyWhenTheStatsWereRead(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.nodes = []workflow.RuntimeAlertSignal{failingNode(3, h.now)}
	h.evaluate(t)
	h.signals.nodes = nil
	probe := workflow.NodeAlertProbe{WorkflowID: "wf-1", NodeID: "n55"}

	h.signals.successesErr = errors.New("db down")
	h.signals.successes = map[workflow.NodeAlertProbe]int{probe: 5}
	h.evaluate(t)
	if h.alerts.only(t).Status != workflow.RuntimeAlertOpen {
		t.Fatal("unreadable stats must not resolve the alert")
	}

	h.signals.successesErr = nil
	h.signals.successes = map[workflow.NodeAlertProbe]int{probe: workflow.NodeRecoverySuccesses}
	h.evaluate(t)
	if h.alerts.only(t).Status != workflow.RuntimeAlertResolved {
		t.Fatal("three successes after the last failure resolve the alert")
	}
}

func TestOneUnreadableSignalSkipsOnlyItsOwnKind(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.deliveriesErr = errors.New("db down")
	h.signals.nodes = []workflow.RuntimeAlertSignal{failingNode(3, h.now)}
	if err := h.uc.Execute(context.Background()); err == nil {
		t.Fatal("the failed query must still be reported")
	}
	if h.alerts.only(t).Key.Kind != workflow.AlertNodeFailing {
		t.Fatal("the node alert must open even when the deliveries could not be read")
	}
}

func TestAnUnreadableSourceNeverResolvesItsAlertsAsQuiet(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.deliveries = []workflow.RejectedDeliveryGroup{{WorkspaceID: "ws-1", WorkflowID: "wf-1", Reason: "unauthorized", Count: workflow.WebhookRejectingMinRejections, FirstSeen: h.now, LastSeen: h.now, Sample: "d-1"}}
	h.signals.toolCalls = []workflow.FailedToolCallGroup{{WorkspaceID: "ws-1", AgentID: "ag-1", ToolName: workflow.AgentHTTPRequestTool, Count: 3, FirstSeen: h.now, LastSeen: h.now, Sample: "c-9"}}
	h.evaluate(t)
	if len(h.alerts.rows) != 2 {
		t.Fatalf("alerts = %+v, want the webhook and the agent tool alerts", h.alerts.rows)
	}

	h.now = h.now.Add(2 * workflow.QuietAlertResolveAfter)
	h.signals.deliveriesErr = errors.New("db down")
	h.signals.toolCallsErr = errors.New("db down")
	if err := h.uc.Execute(context.Background()); err == nil {
		t.Fatal("the failed queries must still be reported")
	}
	for _, alert := range h.alerts.rows {
		if alert.Status != workflow.RuntimeAlertOpen {
			t.Fatalf("%s resolved as quiet although its failures could not be read: %+v", alert.Key.Kind, alert)
		}
	}

	h.signals.deliveriesErr, h.signals.toolCallsErr = nil, nil
	h.signals.deliveries, h.signals.toolCalls = nil, nil
	h.evaluate(t)
	for _, alert := range h.alerts.rows {
		if alert.Status != workflow.RuntimeAlertResolved {
			t.Fatalf("%s must resolve once the source reads quiet: %+v", alert.Key.Kind, alert)
		}
	}
}

func TestNothingIsEvaluatedWhenTheAlertsCannotBeRead(t *testing.T) {
	h := newAlertHarness(t)
	h.alerts.trackErr = errors.New("db down")
	h.signals.nodes = []workflow.RuntimeAlertSignal{failingNode(3, h.now)}
	if err := h.uc.Execute(context.Background()); err == nil {
		t.Fatal("expected the read error")
	}
	if len(h.alerts.rows) != 0 || len(h.notices.Drafts()) != 0 {
		t.Fatal("without the open alerts the evaluator would duplicate them; it must stop")
	}
}

func TestARejectingWebhookAlertLinksToTheAlertSoTheEditorOpensTheFix(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.deliveries = []workflow.RejectedDeliveryGroup{{WorkspaceID: "ws-1", WorkflowID: "wf-1", Reason: "unauthorized", Count: workflow.WebhookRejectingMinRejections, FirstSeen: h.now, LastSeen: h.now, Sample: "d-1"}}
	h.evaluate(t)
	alert := h.alerts.only(t)
	if alert.Key.Kind != workflow.AlertWebhookRejecting || alert.ID == "" {
		t.Fatalf("alert = %+v", alert)
	}
	if d := h.notices.Only(t); d.ActionURL != "/dashboard/workflows/wf-1?alert="+alert.ID || d.ActionLabel != "Ver chamadas" {
		t.Fatalf("draft = %+v", d)
	}
}

func TestAFailingAgentToolNamesTheAgentAndLinksToIt(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.toolCalls = []workflow.FailedToolCallGroup{{WorkspaceID: "ws-1", AgentID: "ag-1", ToolName: workflow.AgentHTTPRequestTool, Count: 3, FirstSeen: h.now, LastSeen: h.now, Sample: "c-9"}}
	h.evaluate(t)
	d := h.notices.Only(t)
	if d.Severity != notification.SeverityWarning || d.ActionURL != "/dashboard/agents/ag-1" || d.ActionLabel != "Abrir agente" || !strings.Contains(d.Title, "Bia") {
		t.Fatalf("draft = %+v", d)
	}
}

func TestEventAlertsFromTheHooksAreNotifiedByTheNextEvaluation(t *testing.T) {
	h := newAlertHarness(t)
	if err := h.alerts.RecordRuntimeAlert(workflow.StuckRunSignal(&workflow.WorkflowRun{ID: "run-3", WorkflowID: "wf-1", WorkspaceID: "ws-1"}, h.now)); err != nil {
		t.Fatal(err)
	}
	if err := h.alerts.RecordRuntimeAlert(workflow.InboundLookupFailedSignal(workflow.TriggerEvent{WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "instagram"}, h.now)); err != nil {
		t.Fatal(err)
	}
	h.evaluate(t)
	byURL := map[string]notification.Draft{}
	for _, d := range h.notices.Drafts() {
		byURL[d.ActionURL] = d
	}
	if d, ok := byURL["/dashboard/workflows/wf-1?run=run-3"]; !ok || d.Severity != notification.SeverityWarning {
		t.Fatalf("stuck run draft missing: %+v", byURL)
	}
	if _, ok := byURL["/dashboard/live-chat?entry=e-1&type=instagram"]; !ok {
		t.Fatalf("inbound draft missing: %+v", byURL)
	}
	for _, d := range h.notices.Drafts() {
		if err := d.Normalize().Validate(); err != nil {
			t.Fatalf("invalid draft %+v: %v", d, err)
		}
	}
}

func TestAnAlertOfADeletedWorkflowResolvesInsteadOfMailing(t *testing.T) {
	h := newAlertHarness(t)
	h.signals.nodes = []workflow.RuntimeAlertSignal{failingNode(3, h.now)}
	delete(h.workflows, "wf-1")
	h.evaluate(t)
	if len(h.notices.Drafts()) != 0 {
		t.Fatal("nobody can fix a deleted workflow, so no email")
	}
	for _, a := range h.alerts.rows {
		if a.Status != workflow.RuntimeAlertResolved {
			t.Fatalf("alert = %+v", a)
		}
	}
}

func TestTheWorkflowListShowsOpenAndRecentlyResolvedAlerts(t *testing.T) {
	h := newAlertHarness(t)
	resolvedAt := h.now.Add(-2 * time.Hour)
	stale := h.now.Add(-25 * time.Hour)
	h.alerts.rows = []workflow.RuntimeAlert{
		{ID: "a-open", Key: workflow.RuntimeAlertKey{WorkspaceID: "ws-1", WorkflowID: "wf-1", Kind: workflow.AlertRunStuck}, Status: workflow.RuntimeAlertOpen},
		{ID: "a-recent", Key: workflow.RuntimeAlertKey{WorkspaceID: "ws-1", WorkflowID: "wf-1", Kind: workflow.AlertRunStuck}, Status: workflow.RuntimeAlertResolved, ResolvedAt: &resolvedAt},
		{ID: "a-old", Key: workflow.RuntimeAlertKey{WorkspaceID: "ws-1", WorkflowID: "wf-1", Kind: workflow.AlertRunStuck}, Status: workflow.RuntimeAlertResolved, ResolvedAt: &stale},
	}
	got, err := h.uc.ForWorkflow(viewer, "ws-1", "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("alerts = %+v, want the open one and the one resolved in the last 24 h", got)
	}
}
