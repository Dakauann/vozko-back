package workflow_repository

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/database/schema"
)

type sqlFixture struct {
	t         *testing.T
	db        *gorm.DB
	workspace string
	at        time.Time
}

func newSQLFixture(t *testing.T, models ...interface{}) *sqlFixture {
	t.Helper()
	tables := append([]interface{}{&schema.WorkflowSchema{}, &schema.WorkflowVersionSchema{}, &schema.WorkflowRunSchema{}, &schema.WorkflowRunLogSchema{}}, models...)
	return &sqlFixture{t: t, db: postgresSchemaTx(t, tables...), workspace: uuid.NewString(), at: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
}

func (f *sqlFixture) exec(statement string, args ...interface{}) {
	f.t.Helper()
	if err := f.db.Exec(statement, args...).Error; err != nil {
		f.t.Fatalf("%s: %v", statement, err)
	}
}

func (f *sqlFixture) workflow(published *int, draft string) string {
	f.t.Helper()
	id := uuid.NewString()
	f.exec(`INSERT INTO workflows (id, workspace_id, name, status, type, trigger_type, trigger_config, graph, version, published_version, created_at, updated_at)
		VALUES (?, ?, 'Fluxo', 'active', 'messages', 'trigger_manual', '{}'::jsonb, ?::jsonb, 1, ?, ?, ?)`, id, f.workspace, draft, published, f.at, f.at)
	return id
}

func (f *sqlFixture) version(workflowID string, number int, graph string) {
	f.t.Helper()
	f.exec(`INSERT INTO workflow_versions (id, workflow_id, version, type, graph, graph_hash, published_at, created_at)
		VALUES (?, ?, ?, 'messages', ?::jsonb, 'h', ?, ?)`, uuid.NewString(), workflowID, number, graph, f.at, f.at)
}

type runSeed struct {
	workflowID, entryID, entryType, status, node, startedBy string
	version                                                 *int
	startedAt                                               time.Time
}

func (f *sqlFixture) run(seed runSeed) string {
	f.t.Helper()
	id := uuid.NewString()
	if seed.entryID == "" {
		seed.entryID = uuid.NewString()
	}
	if seed.entryType == "" {
		seed.entryType = "whatsapp"
	}
	if seed.startedAt.IsZero() {
		seed.startedAt = f.at
	}
	f.exec(`INSERT INTO workflow_runs (id, workflow_id, workspace_id, entry_id, entry_type, status, current_node_id, state, started_at, updated_at, started_by, workflow_version, trigger_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}'::jsonb, ?, ?, ?, ?, '{"webhook":{"cpf":"1"}}'::jsonb)`,
		id, seed.workflowID, f.workspace, seed.entryID, seed.entryType, seed.status, seed.node, seed.startedAt, seed.startedAt, seed.startedBy, seed.version)
	return id
}

func (f *sqlFixture) step(runID, nodeID, status string, at time.Time) {
	f.t.Helper()
	f.exec(`INSERT INTO workflow_run_logs (id, run_id, node_id, node_type, status, executed_at, output) VALUES (?, ?, ?, 'action_http_request', ?, ?, '{"ok":true}'::jsonb)`,
		uuid.NewString(), runID, nodeID, status, at)
}

func graphWithNode(id string, nodeType workflow.NodeType) string {
	return `{"nodes":[{"id":"` + id + `","type":"` + string(nodeType) + `"}],"edges":[]}`
}

func TestTheAgentRunStepIsReadFromItsPinnedVersionNeverFromTheDraft(t *testing.T) {
	f := newSQLFixture(t, &schema.ConversationEvent{}, &schema.ConversationMessage{})
	two, pruned := 2, 1
	cases := []struct {
		name    string
		version *int
		want    workflow.NodeType
	}{
		{"pinned to a stored version", &two, workflow.NodeTypeWaitForReply},
		{"pinned to a pruned version", &pruned, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf := f.workflow(&two, graphWithNode("n7", workflow.NodeTypeActionHTTPRequest))
			f.version(wf, 2, graphWithNode("n7", workflow.NodeTypeWaitForReply))
			entry := uuid.NewString()
			f.run(runSeed{workflowID: wf, entryID: entry, status: "waiting", node: "n7", startedBy: "agent:a-1", version: tc.version})

			got, err := NewAgentCallRunsRepository(f.db).LatestForEntry(f.workspace, entry, "whatsapp")
			if err != nil || got == nil {
				t.Fatalf("run = %+v err = %v", got, err)
			}
			if got.CurrentNodeType != tc.want {
				t.Fatalf("current node type = %q, want %q (a pruned pinned version must never fall back to the draft)", got.CurrentNodeType, tc.want)
			}
		})
	}
}

func TestTheVersionPruneKeepsThePublishedThePinnedAndTheLatest(t *testing.T) {
	f := newSQLFixture(t)
	one := 1
	busy := f.workflow(&one, `{"nodes":[],"edges":[]}`)
	for n := 1; n <= 5; n++ {
		f.version(busy, n, `{"nodes":[],"edges":[]}`)
	}
	two := 2
	f.run(runSeed{workflowID: busy, status: "waiting", version: &two})
	small := f.workflow(&one, `{"nodes":[],"edges":[]}`)
	f.version(small, 1, `{"nodes":[],"edges":[]}`)
	f.version(small, 2, `{"nodes":[],"edges":[]}`)
	gone := f.workflow(&one, `{"nodes":[],"edges":[]}`)
	f.version(gone, 1, `{"nodes":[],"edges":[]}`)
	f.exec(`UPDATE workflows SET deleted_at = now() WHERE id = ?`, gone)

	pruned, err := NewTraceRetentionRepository(f.db).PruneVersions(2, 100)
	if err != nil {
		t.Fatal(err)
	}
	var left []struct {
		WorkflowID string
		Version    int
	}
	if err := f.db.Raw(`SELECT workflow_id, version FROM workflow_versions ORDER BY workflow_id, version`).Scan(&left).Error; err != nil {
		t.Fatal(err)
	}
	kept := map[string][]int{}
	for _, row := range left {
		kept[row.WorkflowID] = append(kept[row.WorkflowID], row.Version)
	}
	if pruned != 2 || !equalInts(kept[busy], []int{1, 2, 4, 5}) || !equalInts(kept[small], []int{1, 2}) || len(kept[gone]) != 0 {
		t.Fatalf("pruned = %d kept = %v; want version 3 of the busy workflow and the deleted workflow's version gone", pruned, kept)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Ints(a)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFailingNodesCountOnlyTheLastExecutionsOfEachNode(t *testing.T) {
	f := newSQLFixture(t)
	wf := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	statuses := []string{"failed", "executed", "failed", "executed", "executed"}
	var lastFailedRun string
	for i, status := range statuses {
		run := f.run(runSeed{workflowID: wf, status: "completed", startedAt: f.at.Add(time.Duration(i) * time.Minute)})
		f.step(run, "n1", status, f.at.Add(time.Duration(i)*time.Minute))
		if status == "failed" {
			lastFailedRun = run
		}
		f.step(run, "n2", "executed", f.at.Add(time.Duration(i)*time.Minute))
	}

	signals, err := NewRuntimeAlertRepository(f.db).FailingNodes(f.at.Add(-time.Hour), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 {
		t.Fatalf("signals = %+v, only n1 ever failed", signals)
	}
	s := signals[0]
	if s.Key.NodeID != "n1" || s.Key.WorkflowID != wf || s.Key.WorkspaceID != f.workspace || s.Count != 1 || s.Sample != lastFailedRun {
		t.Fatalf("signal = %+v, want one failure among the last three executions, sampled from the latest failed run", s)
	}
}

func TestNodeSuccessesAreCountedForTheProbedPairsOnly(t *testing.T) {
	f := newSQLFixture(t)
	wf1 := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	wf2 := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	seed := func(wf, node, status string, minute int) {
		run := f.run(runSeed{workflowID: wf, status: "completed", startedAt: f.at.Add(time.Duration(minute) * time.Minute)})
		f.step(run, node, status, f.at.Add(time.Duration(minute)*time.Minute))
	}
	seed(wf1, "n1", "executed", 0)
	seed(wf1, "n1", "failed", 1)
	seed(wf1, "n1", "executed", 2)
	seed(wf1, "n1", "executed", 3)
	seed(wf1, "n2", "executed", 4)
	seed(wf2, "n1", "executed", 5)
	seed(wf2, "n2", "executed", 6)

	got, err := NewRuntimeAlertRepository(f.db).NodeSuccessesAfterLastFailure([]workflow.NodeAlertProbe{{WorkflowID: wf1, NodeID: "n1"}, {WorkflowID: wf2, NodeID: "n2"}}, f.at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := map[workflow.NodeAlertProbe]int{{WorkflowID: wf1, NodeID: "n1"}: 2, {WorkflowID: wf2, NodeID: "n2"}: 1}
	if len(got) != len(want) || got[workflow.NodeAlertProbe{WorkflowID: wf1, NodeID: "n1"}] != 2 || got[workflow.NodeAlertProbe{WorkflowID: wf2, NodeID: "n2"}] != 1 {
		t.Fatalf("successes = %v, want %v (the tuple IN must not cross workflows and nodes)", got, want)
	}
}

func TestOpenAlertsFollowTheWorkflowDepartmentAndTheConversationRule(t *testing.T) {
	f := newSQLFixture(t, append(visibilityTables(), &schema.WorkflowRuntimeAlertSchema{})...)
	sales, support := uuid.NewString(), uuid.NewString()
	inSales := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	inSupport := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	f.exec(`UPDATE workflows SET department_id = ? WHERE id = ?`, sales, inSales)
	f.exec(`UPDATE workflows SET department_id = ? WHERE id = ?`, support, inSupport)
	alert := func(workflowID, kind, subject, sample string) string {
		id := uuid.NewString()
		f.exec(`INSERT INTO workflow_runtime_alerts (id, workspace_id, workflow_id, node_id, kind, subject, sample, count, first_seen, last_seen, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, 'open', ?, ?)`, id, f.workspace, workflowID, id[:8], kind, subject, sample, f.at, f.at, f.at, f.at)
		return id
	}
	salesNode := alert(inSales, string(workflow.AlertNodeFailing), "", "run-1")
	alert(inSupport, string(workflow.AlertNodeFailing), "", "run-2")
	visibleInbound := alert("", string(workflow.AlertInboundLookupFailed), "whatsapp", f.whatsappEntry(sales))
	alert("", string(workflow.AlertInboundLookupFailed), "whatsapp", f.whatsappEntry(support))
	repo := NewRuntimeAlertRepository(f.db)
	scope := conversation.VisibilityScope{ReadScope: conversation.ReadScope{DepartmentIDs: []string{sales}, RestrictDepartments: true}}

	everyDepartment, err := repo.ListOpen(f.workspace, workflow.DepartmentRestriction{}, scope, shared.Pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if ids := alertIDs(everyDepartment.Items); len(ids) != 3 || !ids[visibleInbound] || !ids[salesNode] {
		t.Fatalf("alerts = %v, want both workflow alerts and only the inbound alert of a visible conversation", ids)
	}

	salesOnly, err := repo.ListOpen(f.workspace, workflow.DepartmentRestriction{Restricted: true, DepartmentIDs: []string{sales}}, scope, shared.Pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if ids := alertIDs(salesOnly.Items); len(ids) != 1 || !ids[salesNode] {
		t.Fatalf("alerts = %v, a sales member sees the sales workflow alerts only", ids)
	}

	forWorkflow, err := repo.ListForWorkflow(f.workspace, inSales, scope, f.at.Add(-time.Hour))
	if err != nil || len(forWorkflow) != 1 || forWorkflow[0].ID != salesNode {
		t.Fatalf("workflow alerts = %+v err = %v", forWorkflow, err)
	}
}

func alertIDs(alerts []*workflow.RuntimeAlert) map[string]bool {
	out := map[string]bool{}
	for _, a := range alerts {
		out[a.ID] = true
	}
	return out
}

func TestThePublishedGraphsOfAWorkspaceAreJoinedInOneReadAgainstPostgres(t *testing.T) {
	f := newSQLFixture(t)
	one, four := 1, 4
	live := f.workflow(&one, graphWithNode("d", workflow.NodeTypeTriggerManual))
	f.version(live, 1, graphWithNode("p", workflow.NodeTypeTriggerWebhook))
	pruned := f.workflow(&four, graphWithNode("d", workflow.NodeTypeTriggerWebhook))
	draft := f.workflow(nil, graphWithNode("d", workflow.NodeTypeTriggerWebhook))
	f.exec(`UPDATE workflows SET status = 'draft' WHERE id = ?`, draft)
	other := f.workflow(&one, graphWithNode("d", workflow.NodeTypeTriggerWebhook))
	f.version(other, 1, graphWithNode("p", workflow.NodeTypeTriggerWebhook))
	f.exec(`UPDATE workflows SET workspace_id = ? WHERE id = ?`, uuid.NewString(), other)
	repo := NewWorkflowRepository(f.db)

	active, err := repo.FindActiveByTrigger(f.workspace, workflow.TriggerWebhook)
	if err != nil || len(active) != 1 || active[0].ID != live || active[0].Graph.Nodes[0].ID != "p" {
		t.Fatalf("active = %+v err = %v, want only the live workflow on its published graph", active, err)
	}

	pairs, err := repo.(workflow.PublishedWorkflows).PublishedInWorkspace(f.workspace)
	if err != nil || len(pairs) != 3 {
		t.Fatalf("pairs = %+v err = %v", pairs, err)
	}
	byID := map[string]workflow.PublishedWorkflow{}
	for _, pair := range pairs {
		byID[pair.Draft.ID] = pair
	}
	if p := byID[live]; p.Published == nil || p.Published.Graph.Nodes[0].ID != "p" || p.Draft.Graph.Nodes[0].ID != "d" {
		t.Fatalf("live = %+v", p)
	}
	if p := byID[pruned]; p.Published != nil || !errors.Is(p.Err, workflow.ErrVersionNotFound) {
		t.Fatalf("pruned = %+v", p)
	}
	if p := byID[draft]; p.Published != nil || p.Err != nil {
		t.Fatalf("draft = %+v", p)
	}
}

func TestAnOperatorCancelChangesOnlyALiveRunsStatusAgainstPostgres(t *testing.T) {
	f := newSQLFixture(t)
	wf := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	live := f.run(runSeed{workflowID: wf, status: "waiting", node: "n3"})
	f.exec(`UPDATE workflow_runs SET state = '{"progress":"step-3"}'::jsonb, wake_at = 1 WHERE id = ?`, live)
	done := f.run(runSeed{workflowID: wf, status: "completed", node: "n9"})
	runs := NewWorkflowRunRepository(f.db).(workflow.RunCanceller)

	if cancelled, err := runs.CancelRun(live, workflow.CancelReasonOperator); err != nil || !cancelled {
		t.Fatalf("cancelled = %v err = %v", cancelled, err)
	}
	if cancelled, err := runs.CancelRun(done, workflow.CancelReasonOperator); err != nil || cancelled {
		t.Fatalf("a finished run must stay finished: cancelled = %v err = %v", cancelled, err)
	}
	var row struct {
		Status        string
		Error         string
		CurrentNodeID string
		State         string
		WakeAt        *int64
		CompletedAt   *time.Time
	}
	if err := f.db.Raw(`SELECT status, error, current_node_id, state::text AS state, wake_at, completed_at FROM workflow_runs WHERE id = ?`, live).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "cancelled" || row.Error != workflow.CancelReasonOperator || row.CurrentNodeID != "n3" || row.State != `{"progress": "step-3"}` || row.WakeAt != nil || row.CompletedAt == nil {
		t.Fatalf("row = %+v", row)
	}
}

func TestTriggerDataFollowsTheThirtyDayPayloadRetentionAgainstPostgres(t *testing.T) {
	f := newSQLFixture(t)
	wf := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	old := f.run(runSeed{workflowID: wf, status: "waiting", startedAt: f.at.Add(-31 * 24 * time.Hour)})
	recent := f.run(runSeed{workflowID: wf, status: "completed", startedAt: f.at.Add(-24 * time.Hour)})

	pruned, err := NewTraceRetentionRepository(f.db).PruneTriggerData(f.at.Add(-30*24*time.Hour), 100)
	if err != nil || pruned != 1 {
		t.Fatalf("pruned = %d err = %v", pruned, err)
	}
	var kept []string
	if err := f.db.Raw(`SELECT id FROM workflow_runs WHERE trigger_data IS NOT NULL`).Scan(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0] != recent {
		t.Fatalf("runs keeping trigger data = %v, want only %s (not %s)", kept, recent, old)
	}
}

func visibilityTables() []interface{} {
	return []interface{}{
		&schema.WhatsAppCampaign{}, &schema.WhatsAppCampaignEntry{}, &schema.InstagramAccount{}, &schema.InstagramConversation{},
		&schema.FacebookPage{}, &schema.FacebookConversation{}, &schema.TelegramAccount{}, &schema.TelegramConversation{},
		&schema.WebchatWidget{}, &schema.WebchatConversation{}, &schema.MercadoLivreAccount{}, &schema.MercadoLivreConversation{},
		&schema.TikTokAccount{}, &schema.TikTokConversation{}, &schema.UnofficialWhatsAppInstance{}, &schema.UnofficialWhatsAppConversation{},
		&schema.InboxAssignment{},
	}
}

func (f *sqlFixture) whatsappEntry(departmentID string) string {
	f.t.Helper()
	campaign, entry := uuid.NewString(), uuid.NewString()
	f.exec(`INSERT INTO whatsapp_campaigns (id, workspace_id, department_id, name) VALUES (?, ?, ?, 'Campanha')`, campaign, f.workspace, departmentID)
	f.exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id) VALUES (?, ?, ?)`, entry, campaign, uuid.NewString())
	return entry
}

func TestTheRunListShowsOnlyRunsOfConversationsTheViewerCanSee(t *testing.T) {
	f := newSQLFixture(t, append(visibilityTables(), &schema.WorkflowWebhookDeliverySchema{})...)
	sales, support, viewer := uuid.NewString(), uuid.NewString(), uuid.NewString()
	wf := f.workflow(nil, `{"nodes":[],"edges":[]}`)
	ownDepartment := f.run(runSeed{workflowID: wf, entryID: f.whatsappEntry(sales), status: "completed"})
	assignedElsewhere := f.whatsappEntry(support)
	f.exec(`INSERT INTO inbox_assignments (id, workspace_id, entry_id, entry_type, assigned_user_id) VALUES (?, ?, ?, 'whatsapp', ?)`, uuid.NewString(), f.workspace, assignedElsewhere, viewer)
	assigned := f.run(runSeed{workflowID: wf, entryID: assignedElsewhere, status: "completed"})
	f.run(runSeed{workflowID: wf, entryID: f.whatsappEntry(support), status: "completed"})
	f.run(runSeed{workflowID: wf, entryID: uuid.NewString(), status: "completed"})
	voice := f.run(runSeed{workflowID: wf, entryType: workflow.EntryTypeSIPCall, status: "completed"})

	scope := conversation.VisibilityScope{ReadScope: conversation.ReadScope{DepartmentIDs: []string{sales}, RestrictDepartments: true, AssigneeOverrideUserID: viewer}}
	page, err := NewWorkflowRunRepository(f.db).List(workflow.ListRunsInput{WorkspaceID: f.workspace, WorkflowID: wf, Visibility: scope, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, run := range page.Items {
		got[run.ID] = true
	}
	if page.TotalItems != 3 || len(got) != 3 || !got[ownDepartment] || !got[assigned] || !got[voice] {
		t.Fatalf("visible runs = %v (total %d); want the own department, the assigned conversation and the voice run", got, page.TotalItems)
	}

	deliveries := NewWebhookDeliveryRepository(f.db)
	unresolved, visibleDelivery := uuid.NewString(), uuid.NewString()
	for id, entry := range map[string]string{unresolved: "", visibleDelivery: f.whatsappEntry(sales), uuid.NewString(): f.whatsappEntry(support)} {
		entryType := "whatsapp"
		if entry == "" {
			entryType = ""
		}
		f.exec(`INSERT INTO workflow_webhook_deliveries (id, webhook_id, workflow_id, workspace_id, received_at, outcome, http_status, entry_id, entry_type, latency_ms)
			VALUES (?, ?, ?, ?, ?, 'rejected', 409, ?, ?, 1)`, id, uuid.NewString(), wf, f.workspace, f.at, entry, entryType)
	}
	deliveryPage, err := deliveries.ListByWorkflow(f.workspace, wf, scope, shared.Pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	seenDeliveries := map[string]bool{}
	for _, d := range deliveryPage.Items {
		seenDeliveries[d.ID] = true
	}
	if len(seenDeliveries) != 2 || !seenDeliveries[unresolved] || !seenDeliveries[visibleDelivery] {
		t.Fatalf("deliveries = %v, want the unresolved one and the one in the viewer's department", seenDeliveries)
	}

	nothing, err := NewWorkflowRunRepository(f.db).List(workflow.ListRunsInput{WorkspaceID: f.workspace, WorkflowID: wf, Visibility: conversation.NothingVisible(), Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	if nothing.TotalItems != 1 || nothing.Items[0].ID != voice {
		t.Fatalf("an unreadable scope must show no conversation run, got %+v", nothing.Items)
	}
}
