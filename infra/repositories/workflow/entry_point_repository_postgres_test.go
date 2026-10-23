package workflow_repository

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/workflow"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type entryPointFixture struct {
	db        *gorm.DB
	workspace string
	other     string
	published string
	draft     string
	elsewhere string
	at        time.Time
}

func entryPointTables() []interface{} {
	return []interface{}{
		&schema.WorkflowSchema{}, &schema.WorkflowVersionSchema{}, &schema.WorkflowRunSchema{},
		&schema.WorkflowWebhookSchema{}, &schema.WorkflowWebhookDeliverySchema{}, &schema.Agent{}, &schema.WhatsAppForm{},
		&schema.WhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaign{}, &schema.UnofficialWhatsAppInstance{},
		&schema.FacebookPage{}, &schema.InstagramAccount{}, &schema.TelegramAccount{}, &schema.TikTokAccount{},
		&schema.MercadoLivreAccount{}, &schema.WebchatWidget{}, &schema.TemplateButtonAction{}, &schema.WhatsAppCampaignButtonRoutes{},
		&schema.EmailInbox{}, &schema.RCSSender{}, &schema.SMSRoute{},
	}
}

func (f *entryPointFixture) create(t *testing.T, rows ...interface{}) {
	t.Helper()
	for _, row := range rows {
		if err := f.db.Omit(clause.Associations).Create(row).Error; err != nil {
			t.Fatalf("create %T: %v", row, err)
		}
	}
}

func jsonOf(t *testing.T, v interface{}) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func strPtr(s string) *string { return &s }

func agentRow(workspaceID, name, bindings string) *schema.Agent {
	return &schema.Agent{ID: uuid.NewString(), WorkspaceID: workspaceID, Name: name, MessagingPrompt: "p", IsActive: true,
		Tags: "[]", Metadata: "{}", MediaIDs: "[]", Variables: "[]", ToolBindings: bindings}
}

func runWorkflowBindings(t *testing.T, workflowID string) string {
	return jsonOf(t, map[string]interface{}{"bindings": []map[string]interface{}{
		{"name": "http_request", "config": map[string]interface{}{"workflow_id": workflowID}},
		{"name": workflow.AgentRunWorkflowTool, "config": map[string]interface{}{"workflow_id": workflowID}},
	}})
}

func seedEntryPoints(t *testing.T) *entryPointFixture {
	t.Helper()
	db := postgresSchemaTx(t, entryPointTables()...)
	f := &entryPointFixture{db: db, workspace: uuid.NewString(), other: uuid.NewString(), published: uuid.NewString(), draft: uuid.NewString(), elsewhere: uuid.NewString(), at: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	one := 1
	publishedGraph := workflow.Graph{Nodes: []workflow.Node{
		{ID: "agent", Type: workflow.NodeTypeTriggerAgentCall, Config: map[string]interface{}{}},
		{ID: "hook", Type: workflow.NodeTypeTriggerWebhook},
		{ID: "end", Type: workflow.NodeTypeEnd},
	}}
	draftGraph := workflow.Graph{Nodes: []workflow.Node{{ID: "message", Type: workflow.NodeTypeTriggerMessageReceived}, {ID: "end", Type: workflow.NodeTypeEnd}}}
	f.create(t,
		&schema.WorkflowSchema{ID: f.published, WorkspaceID: f.workspace, Name: "Matrícula", Status: "active", Type: "messages", TriggerConfig: "{}", Graph: jsonOf(t, draftGraph), Version: 3, PublishedVersion: &one},
		&schema.WorkflowVersionSchema{WorkflowID: f.published, Version: 1, Type: "messages", Graph: jsonOf(t, publishedGraph), GraphHash: "h", PublishedAt: f.at},
		&schema.WorkflowSchema{ID: f.draft, WorkspaceID: f.workspace, Name: "Rascunho", Status: "draft", Type: "messages", TriggerConfig: "{}", Graph: jsonOf(t, draftGraph), Version: 1},
		&schema.WorkflowWebhookSchema{WorkflowID: f.published, WorkspaceID: f.workspace, Token: uuid.NewString(), AuthMode: "header_token", Method: "POST", Active: true},
		&schema.WorkflowWebhookDeliverySchema{ID: uuid.NewString(), WebhookID: uuid.NewString(), WorkflowID: f.published, WorkspaceID: f.workspace, ReceivedAt: f.at.Add(-2 * time.Hour), Outcome: "accepted", HTTPStatus: 202},
		&schema.WorkflowWebhookDeliverySchema{ID: uuid.NewString(), WebhookID: uuid.NewString(), WorkflowID: f.published, WorkspaceID: f.workspace, ReceivedAt: f.at, Outcome: "rejected", Reason: workflow.WorkflowNotActiveCode, HTTPStatus: 409},
	)
	for i, run := range []struct {
		startedBy string
		trigger   workflow.TriggerType
		at        time.Time
		workflow  string
	}{
		{workflow.StartedByAgent("a-1"), workflow.TriggerAgentCall, f.at.Add(-3 * time.Hour), f.published},
		{workflow.StartedByAgent("a-1"), workflow.TriggerAgentCall, f.at.Add(-time.Hour), f.published},
		{workflow.StartedByExit("m-1"), workflow.TriggerAgentCall, f.at.Add(-30 * time.Minute), f.published},
		{workflow.StartedByManual("u-1"), workflow.TriggerAgentCall, f.at.Add(-20 * time.Minute), f.published},
		{workflow.StartedByWebhook("d-1"), workflow.TriggerWebhook, f.at.Add(-10 * time.Minute), f.published},
		{workflow.StartedByAgent("a-1"), workflow.TriggerAgentCall, f.at, f.elsewhere},
	} {
		f.create(t, &schema.WorkflowRunSchema{ID: uuid.NewString(), WorkflowID: run.workflow, WorkspaceID: f.workspace, EntryID: uuid.NewString(), EntryType: "whatsapp", Status: "completed", State: "{}", StartedAt: run.at, StartedBy: run.startedBy, TriggerType: string(run.trigger), TriggerNodeID: "n" + string(rune('0'+i))})
	}
	return f
}

func TestEntryPointSourcesAgainstPostgres(t *testing.T) {
	f := seedEntryPoints(t)
	repo := NewEntryPointRepository(f.db)

	triggers, err := repo.PublishedTriggers([]string{f.published, f.draft})
	if err != nil {
		t.Fatal(err)
	}
	if got := triggers[f.published]; len(got) != 2 || got[0].ID != "agent" || got[1].Type != workflow.NodeTypeTriggerWebhook {
		t.Fatalf("published triggers = %+v, want the two triggers of version 1 and not the draft", got)
	}
	if _, ok := triggers[f.draft]; ok {
		t.Fatal("a never published workflow has no published triggers")
	}

	sources, err := repo.EntryPointSources(workflow.EntryPointSourceQuery{WorkspaceID: f.workspace, WorkflowIDs: []string{f.published, f.draft}})
	if err != nil {
		t.Fatal(err)
	}
	if hook, ok := sources.Webhooks[f.published]; !ok || !hook.Active || hook.AuthMode != workflow.WebhookAuthHeaderToken || hook.Method != "POST" {
		t.Fatalf("webhook = %+v", sources.Webhooks)
	}
	if d := sources.LastDeliveries[f.published]; !d.At.Equal(f.at) || d.Outcome != workflow.WebhookOutcomeRejected || d.Reason != workflow.WorkflowNotActiveCode {
		t.Fatalf("last delivery = %+v, want the latest, a rejection", d)
	}
	starts := sources.LastStarts[f.published]
	for kind, want := range map[workflow.EntryPointKind]time.Time{
		workflow.EntryAgentCall: f.at.Add(-time.Hour),
		workflow.EntryLeadExit:  f.at.Add(-30 * time.Minute),
		workflow.EntryWebhook:   f.at.Add(-10 * time.Minute),
	} {
		if !starts[kind].Equal(want) {
			t.Fatalf("last %s = %v, want %v (starts %+v)", kind, starts[kind], want, starts)
		}
	}
	for kind, at := range starts {
		if at.Equal(f.at.Add(-20 * time.Minute)) {
			t.Fatalf("an operator start counted as the last call of %s: %+v", kind, starts)
		}
	}
	if len(sources.LastStarts[f.draft]) != 0 {
		t.Fatalf("draft starts = %+v", sources.LastStarts[f.draft])
	}
}

func TestTheLatestRunPerEntryKindIsReadThroughItsIndex(t *testing.T) {
	f := seedEntryPoints(t)
	ddl, ok := database.ConcurrentIndexSQL(database.WorkflowRunOriginIndex)
	if !ok {
		t.Fatalf("%s is not declared", database.WorkflowRunOriginIndex)
	}
	if err := f.db.Exec(strings.Replace(ddl, "CONCURRENTLY ", "", 1)).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
		t.Fatal(err)
	}
	var plan []string
	if err := f.db.Raw("EXPLAIN "+lastStartsSQL, []string{f.published}).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(plan, "\n"); !strings.Contains(joined, database.WorkflowRunOriginIndex) {
		t.Fatalf("plan does not use %s:\n%s", database.WorkflowRunOriginIndex, joined)
	}
}

func TestAgentBindingsAndTemplateExitsAreReadThroughTheirIndexes(t *testing.T) {
	f := seedEntryPoints(t)
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
	for _, tc := range []struct {
		index string
		sql   string
		args  []interface{}
	}{
		{database.AgentRunWorkflowBindingsIndex, boundAgentsSQL, []interface{}{f.workspace, workflow.AgentRunWorkflowTool, []string{f.published}}},
		{"template_button_actions_pkey", templateExitsSQL, []interface{}{f.workspace, []string{f.published}}},
	} {
		var plan []string
		if err := f.db.Raw("EXPLAIN "+tc.sql, tc.args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		if joined := strings.Join(plan, "\n"); !strings.Contains(joined, tc.index) {
			t.Fatalf("plan does not use %s:\n%s", tc.index, joined)
		}
	}
}

func TestEntryPointBindingsAgainstPostgres(t *testing.T) {
	f := seedEntryPoints(t)
	other := uuid.NewString()
	published := uuid.NewString()
	draftForm := uuid.NewString()
	archivedForm := uuid.NewString()
	foreignForm := uuid.NewString()
	now := f.at
	f.create(t,
		agentRow(f.workspace, "Bia", runWorkflowBindings(t, f.published)),
		agentRow(f.workspace, "Arquivada", runWorkflowBindings(t, f.published)),
		agentRow(f.other, "Outro workspace", runWorkflowBindings(t, f.published)),
		agentRow(f.workspace, "Outro fluxo", runWorkflowBindings(t, other)),
		agentRow(f.workspace, "Legado", `{"run_workflow":"x"}`),
		agentRow(f.workspace, "Sem ferramentas", `{"bindings":[]}`),
		agentRow(f.workspace, "Ana", runWorkflowBindings(t, f.published)),
		&schema.WhatsAppForm{ID: published, WorkspaceID: f.workspace, Name: "Matrícula", Kind: "survey", DraftVersionID: uuid.NewString(), PublishedVersionID: schema.OptionalText(uuid.NewString()), CreatedAt: now, UpdatedAt: now},
		&schema.WhatsAppForm{ID: draftForm, WorkspaceID: f.workspace, Name: "Rascunho", Kind: "survey", DraftVersionID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		&schema.WhatsAppForm{ID: archivedForm, WorkspaceID: f.workspace, Name: "Arquivado", Kind: "survey", DraftVersionID: uuid.NewString(), PublishedVersionID: schema.OptionalText(uuid.NewString()), CreatedAt: now, UpdatedAt: now, ArchivedAt: &now},
		&schema.WhatsAppForm{ID: foreignForm, WorkspaceID: f.other, Name: "Alheio", Kind: "survey", DraftVersionID: uuid.NewString(), PublishedVersionID: schema.OptionalText(uuid.NewString()), CreatedAt: now, UpdatedAt: now},
	)
	boundCampaign, chosenCampaign, openCampaign, archivedCampaign := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	page, ig, widget, bot, deletedInstance, foreignWidget := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	f.create(t,
		&schema.WhatsAppCampaign{ID: boundCampaign, WorkspaceID: f.workspace, Name: "Black Friday", WorkflowID: strPtr(f.published), EnableWorkflow: true},
		&schema.WhatsAppCampaign{ID: chosenCampaign, WorkspaceID: f.workspace, Name: "Escolhida", WorkflowID: strPtr(other)},
		&schema.WhatsAppCampaign{ID: openCampaign, WorkspaceID: f.workspace, Name: "Aberta", EnableWorkflow: true},
		&schema.UnofficialWhatsAppCampaign{ID: archivedCampaign, WorkspaceID: f.workspace, Name: "Arquivada", WorkflowID: strPtr(f.published), EnableWorkflow: true, Archived: true},
		&schema.FacebookPage{ID: page, WorkspaceID: f.workspace, GrantID: uuid.NewString(), FBPageID: uuid.NewString()[:20], Name: "Loja", WorkflowID: strPtr(f.published), EnableWorkflow: true},
		&schema.InstagramAccount{ID: ig, WorkspaceID: f.workspace, IGUserID: uuid.NewString()[:20], Username: "loja", WorkflowID: strPtr(f.published)},
		&schema.WebchatWidget{ID: widget, WorkspaceID: f.workspace, Name: "Site", EnableWorkflow: true},
		&schema.TelegramAccount{ID: bot, WorkspaceID: f.workspace, BotUserID: 42, BotUsername: "lojabot", WorkflowID: strPtr(other), EnableWorkflow: true},
		&schema.UnofficialWhatsAppInstance{ID: deletedInstance, WorkspaceID: f.workspace, ServerID: uuid.NewString(), DisplayName: "Apagada", WorkflowID: strPtr(f.published), EnableWorkflow: true, DeletedAt: gorm.DeletedAt{Time: now, Valid: true}},
		&schema.WebchatWidget{ID: foreignWidget, WorkspaceID: f.other, Name: "Alheio", WorkflowID: strPtr(f.published), EnableWorkflow: true},
		&schema.TemplateButtonAction{WorkspaceID: f.workspace, TemplateName: "boas_vindas", Language: "pt_BR", ButtonIndex: 0, Kind: "start_workflow", Params: []byte(jsonOf(t, map[string]string{"workflowId": f.published})), ButtonText: "Quero falar", Version: 1, UpdatedAt: now},
		&schema.TemplateButtonAction{WorkspaceID: f.workspace, TemplateName: "boas_vindas", Language: "pt_BR", ButtonIndex: 1, Kind: "hand_off", Params: []byte(jsonOf(t, map[string]string{"workflowId": f.published})), ButtonText: "Atendente", Version: 1, UpdatedAt: now},
		&schema.TemplateButtonAction{WorkspaceID: f.other, TemplateName: "alheio", Language: "pt_BR", ButtonIndex: 0, Kind: "start_workflow", Params: []byte(jsonOf(t, map[string]string{"workflowId": f.published})), ButtonText: "Alheio", Version: 1, UpdatedAt: now},
		&schema.WhatsAppCampaignButtonRoutes{CampaignID: boundCampaign, WorkspaceID: f.workspace, TemplateName: "promo", Language: "pt_BR", Version: 1, UpdatedAt: now, Routes: []byte(jsonOf(t, []map[string]interface{}{
			{"buttonIndex": 0, "buttonText": "Matricular", "kind": "start_workflow", "params": map[string]string{"workflowId": f.published}},
			{"buttonIndex": 1, "buttonText": "Sair", "kind": "opt_out", "params": map[string]string{}},
		}))},
	)

	for name, column := range map[string]string{"Ana": "is_active", "Arquivada": "archived"} {
		value := column == "archived"
		if err := f.db.Table("agents").Where("name = ?", name).Update(column, value).Error; err != nil {
			t.Fatal(err)
		}
	}

	repo := NewEntryPointRepository(f.db)
	sources, err := repo.EntryPointSources(workflow.EntryPointSourceQuery{
		WorkspaceID: f.workspace, WorkflowIDs: []string{f.published, f.draft},
		FormIDs: []string{published, draftForm, archivedForm, foreignForm, "not-a-uuid"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var agents []string
	for _, a := range sources.Agents[f.published] {
		agents = append(agents, a.Name+map[bool]string{true: "+", false: "-"}[a.Active])
	}
	if strings.Join(agents, ",") != "Ana-,Bia+" {
		t.Fatalf("agents = %v, want only this workspace's live agents binding run_workflow to it", agents)
	}

	if got := sources.Forms; len(got) != 3 || !got[published].Active || got[draftForm].Active || got[archivedForm].Active || got[published].Name != "Matrícula" {
		t.Fatalf("forms = %+v, want this workspace's forms, live only when published and not archived", got)
	}

	var linked []string
	for _, c := range sources.LinkedChannels {
		linked = append(linked, string(c.Kind)+":"+c.Name+":"+map[bool]string{true: "on", false: "off"}[c.WorkflowEnabled])
	}
	sort.Strings(linked)
	if want := "facebook_page:Loja:on,instagram_account:loja:off,whatsapp_campaign:Black Friday:on"; strings.Join(linked, ",") != want {
		t.Fatalf("linked channels = %v\nwant %s", linked, want)
	}
	enabled := map[string]string{}
	for _, c := range sources.EnabledChannels {
		enabled[c.Name] = c.WorkflowID
	}
	if len(enabled) != 5 || enabled["Site"] != "" || enabled["Aberta"] != "" || enabled["lojabot"] != other || enabled["Black Friday"] != f.published || enabled["Loja"] != f.published {
		t.Fatalf("enabled channels = %+v", enabled)
	}

	exits := sources.LeadExits[f.published]
	if len(exits) != 2 || exits[0] != (workflow.LeadExitEntry{Source: workflow.LeadExitFromCampaign, ID: boundCampaign, Name: "Black Friday", Button: "Matricular"}) ||
		exits[1] != (workflow.LeadExitEntry{Source: workflow.LeadExitFromTemplate, Name: "boas_vindas", Language: "pt_BR", Button: "Quero falar"}) {
		t.Fatalf("lead exits = %+v", exits)
	}

	triggers, err := repo.PublishedTriggers([]string{f.published})
	if err != nil {
		t.Fatal(err)
	}
	w := &workflow.Workflow{ID: f.published, WorkspaceID: f.workspace, Status: workflow.WorkflowStatusActive, PublishedVersion: func() *int { one := 1; return &one }()}
	entries := workflow.EntryPointsOf(w, workflow.RunningTriggers(w, triggers), sources.FactsOf(f.published))
	kinds := map[workflow.EntryPointKind]workflow.EntryPoint{}
	for _, e := range entries {
		kinds[e.Kind] = e
	}
	if kinds[workflow.EntryAgentCall].Readiness != workflow.EntryReady || kinds[workflow.EntryWebhook].Readiness != workflow.EntryReady || kinds[workflow.EntryLeadExit].Reason != workflow.EntryBlockedNeedsMessageTrigger || len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
}
