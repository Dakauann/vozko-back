package workflow_repository

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

func graphOf(t *testing.T, triggers ...workflow.NodeType) string {
	nodes := []workflow.Node{{ID: "end", Type: workflow.NodeTypeEnd}}
	for i, trigger := range triggers {
		nodes = append(nodes, workflow.Node{ID: "t" + string(rune('a'+i)), Type: trigger, Config: map[string]interface{}{}})
	}
	return jsonOf(t, workflow.Graph{Nodes: nodes})
}

func seedEntryKinds(t *testing.T) (*entryPointFixture, map[string]string) {
	t.Helper()
	f := &entryPointFixture{db: postgresSchemaTx(t, entryPointTables()...), workspace: uuid.NewString(), other: uuid.NewString(), at: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	names := map[string]string{}
	published := func(name string, draft, running string, version int, withVersion bool) string {
		id := uuid.NewString()
		names[id] = name
		f.create(t, &schema.WorkflowSchema{ID: id, WorkspaceID: f.workspace, Name: name, Status: "active", Type: "messages", TriggerConfig: "{}", Graph: draft, Version: 2, PublishedVersion: &version})
		if withVersion {
			f.create(t, &schema.WorkflowVersionSchema{WorkflowID: id, Version: version, Type: "messages", Graph: running, GraphHash: "h", PublishedAt: f.at})
		}
		return id
	}
	neverPublished := func(name string, draft string) string {
		id := uuid.NewString()
		names[id] = name
		f.create(t, &schema.WorkflowSchema{ID: id, WorkspaceID: f.workspace, Name: name, Status: "draft", Type: "messages", TriggerConfig: "{}", Graph: draft, Version: 1})
		return id
	}

	hook := published("hook", graphOf(t, workflow.NodeTypeTriggerManual), graphOf(t, workflow.NodeTypeTriggerWebhook), 1, true)
	draftOnly := neverPublished("draft", graphOf(t, workflow.NodeTypeTriggerManual))
	unboundHook := published("unbound hook", graphOf(t), graphOf(t, workflow.NodeTypeTriggerMessageReceived), 1, true)
	agentBound := published("agent bound", graphOf(t), graphOf(t, workflow.NodeTypeTriggerNoReply), 1, true)
	idleAgent := published("idle agent", graphOf(t), graphOf(t, workflow.NodeTypeTriggerFirstMessage), 1, true)
	campaignExit := neverPublished("campaign exit", graphOf(t, workflow.NodeTypeTriggerStageAdded))
	templateExit := neverPublished("template exit", graphOf(t, workflow.NodeTypeTriggerFirstMessage))
	closedHook := published("closed hook", graphOf(t), graphOf(t, workflow.NodeTypeTriggerFormSubmitted), 1, true)
	_ = published("pruned version", graphOf(t, workflow.NodeTypeTriggerCallReceived), "", 5, false)
	_ = neverPublished("campaign sent", graphOf(t, workflow.NodeTypeTriggerCampaignSent, workflow.NodeTypeTriggerAgentCall))
	foreign := uuid.NewString()
	f.create(t, &schema.WorkflowSchema{ID: foreign, WorkspaceID: f.other, Name: "foreign", Status: "draft", Type: "messages", TriggerConfig: "{}", Graph: graphOf(t, workflow.NodeTypeTriggerWebhook), Version: 1})

	campaign := uuid.NewString()
	active, idle := agentRow(f.workspace, "Bia", runWorkflowBindings(t, agentBound)), agentRow(f.workspace, "Ana", runWorkflowBindings(t, idleAgent))
	f.create(t,
		&schema.WorkflowWebhookSchema{WorkflowID: unboundHook, WorkspaceID: f.workspace, Token: uuid.NewString(), AuthMode: "header_token", Method: "POST", Active: true},
		&schema.WorkflowWebhookSchema{WorkflowID: closedHook, WorkspaceID: f.workspace, Token: uuid.NewString(), AuthMode: "header_token", Method: "POST", Active: true},
		active, idle,
		agentRow(f.other, "Alheia", runWorkflowBindings(t, hook)),
		&schema.WhatsAppCampaign{ID: campaign, WorkspaceID: f.workspace, Name: "Promo"},
		&schema.WhatsAppCampaignButtonRoutes{CampaignID: campaign, WorkspaceID: f.workspace, TemplateName: "promo", Language: "pt_BR", Version: 1, UpdatedAt: f.at, Routes: []byte(jsonOf(t, []map[string]interface{}{
			{"buttonIndex": 0, "buttonText": "Matricular", "kind": "start_workflow", "params": map[string]string{"workflowId": campaignExit}},
		}))},
		&schema.TemplateButtonAction{WorkspaceID: f.workspace, TemplateName: "boas_vindas", Language: "pt_BR", ButtonIndex: 0, Kind: "start_workflow", Params: []byte(jsonOf(t, map[string]string{"workflowId": templateExit})), ButtonText: "Quero", Version: 1, UpdatedAt: f.at},
		&schema.TemplateButtonAction{WorkspaceID: f.workspace, TemplateName: "boas_vindas", Language: "pt_BR", ButtonIndex: 1, Kind: "hand_off", Params: []byte(jsonOf(t, map[string]string{"workflowId": draftOnly})), ButtonText: "Atendente", Version: 1, UpdatedAt: f.at},
	)
	if err := f.db.Table("workflow_webhooks").Where("workflow_id = ?", closedHook).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Table("agents").Where("id = ?", idle.ID).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	return f, names
}

func readModelKinds(t *testing.T, f *entryPointFixture, workflows []*workflow.Workflow) map[workflow.EntryPointKind][]string {
	t.Helper()
	reader := NewEntryPointRepository(f.db)
	var published []string
	for _, w := range workflows {
		if w.PublishedVersion != nil {
			published = append(published, w.ID)
		}
	}
	triggers, err := reader.PublishedTriggers(published)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := reader.EntryPointSources(workflow.EntryPointQueryOf(f.workspace, workflows, triggers))
	if err != nil {
		t.Fatal(err)
	}
	out := map[workflow.EntryPointKind][]string{}
	for _, w := range workflows {
		seen := map[workflow.EntryPointKind]bool{}
		for _, entry := range workflow.EntryPointsOf(w, workflow.RunningTriggers(w, triggers), sources.FactsOf(w.ID)) {
			if !seen[entry.Kind] {
				seen[entry.Kind] = true
				out[entry.Kind] = append(out[entry.Kind], w.ID)
			}
		}
	}
	return out
}

func sortedNames(ids []string, names map[string]string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, names[id])
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestTheEntryKindFilterAgreesWithTheReadModelOnPostgres(t *testing.T) {
	f, names := seedEntryKinds(t)
	repo := NewWorkflowRepository(f.db)
	all, err := repo.List(workflow.ListWorkflowsInput{WorkspaceID: f.workspace, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	expected := readModelKinds(t, f, all.Items)

	for _, kind := range workflow.EntryPointKinds() {
		t.Run(string(kind), func(t *testing.T) {
			k := kind
			page, err := repo.List(workflow.ListWorkflowsInput{WorkspaceID: f.workspace, EntryKind: &k, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 100}}})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, w := range page.Items {
				got = append(got, w.ID)
			}
			if sortedNames(got, names) != sortedNames(expected[kind], names) || page.TotalItems != int64(len(expected[kind])) {
				t.Fatalf("filter = [%s] (total %d), read model = [%s]", sortedNames(got, names), page.TotalItems, sortedNames(expected[kind], names))
			}
		})
	}

	want := map[workflow.EntryPointKind]string{
		workflow.EntryWebhook:         "hook,unbound hook",
		workflow.EntryManual:          "draft",
		workflow.EntryAgentCall:       "agent bound,campaign sent",
		workflow.EntryLeadExit:        "campaign exit,template exit",
		workflow.EntryCallReceived:    "",
		workflow.EntryMessageReceived: "unbound hook",
	}
	for kind, wanted := range want {
		if got := sortedNames(expected[kind], names); got != wanted {
			t.Fatalf("fixture drifted for %s: %s, want %s", kind, got, wanted)
		}
	}
}

func TestTheEntryKindFilterReadsItsBindingsThroughTheirIndexes(t *testing.T) {
	f, _ := seedEntryKinds(t)
	ddl, ok := database.PerformanceIndexSQL(database.AgentRunWorkflowBindingsIndex)
	if !ok {
		t.Fatalf("%s is not declared", database.AgentRunWorkflowBindingsIndex)
	}
	if err := f.db.Exec(ddl).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range workflow.EntryPointKinds() {
		filter, args := entryKindFilter(f.workspace, kind)
		var plan []string
		if err := f.db.Raw("EXPLAIN SELECT id FROM workflows WHERE workspace_id = ? AND "+filter, append([]interface{}{f.workspace}, args...)...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		if joined := strings.Join(plan, "\n"); strings.Contains(joined, "Seq Scan") {
			t.Fatalf("%s: a binding read has no supporting index:\n%s", kind, joined)
		}
	}
}

func TestTheEntryKindFilterPagesInSQL(t *testing.T) {
	f, _ := seedEntryKinds(t)
	kind := workflow.EntryLeadExit
	page, err := NewWorkflowRepository(f.db).List(workflow.ListWorkflowsInput{WorkspaceID: f.workspace, EntryKind: &kind, Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 2, PageSize: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 2 || len(page.Items) != 1 || page.TotalPages != 2 {
		t.Fatalf("page = %+v items = %d", page, len(page.Items))
	}
}
