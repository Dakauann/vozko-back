package campaignaudiencewiring_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/agent"
	"vozko/domain/buttonaction"
	"vozko/domain/cache"
	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/whatsapp_campaign/smartcampaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
	"vozko/domain/working_hours"
	"vozko/domain/workspace_template_access"
	infracache "vozko/infra/cache"
	"vozko/infra/campaignaudiencewiring"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	customfield_repository "vozko/infra/repositories/customfield"
	lead_repository "vozko/infra/repositories/lead"
	lead_campaign_send_repository "vozko/infra/repositories/lead_campaign_send"
	"vozko/infra/repositories/repotest"
	uwc_repository "vozko/infra/repositories/unofficial_whatsapp_campaign"
	whatsapp_repository "vozko/infra/repositories/whatsapp"
	wc_repository "vozko/infra/repositories/whatsapp_campaign"
	wce_repository "vozko/infra/repositories/whatsapp_campaign_entry"
	wave_repository "vozko/infra/repositories/whatsapp_campaign_wave"
	workspace_config_repository "vozko/infra/repositories/workspace_config"
	template_access_repository "vozko/infra/repositories/workspace_template_access"
	"vozko/infra/testenv"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	"vozko/usecases/campaignaudience/audiencecheck"
	"vozko/usecases/campaignautomation"
	"vozko/usecases/campaigncreate"
	"vozko/usecases/campaignguard"
	"vozko/usecases/emailhygiene/hygienetest"
	lead_usecase "vozko/usecases/lead"
	smartcampaign_usecase "vozko/usecases/smartcampaign"
	uwuc "vozko/usecases/unofficial_whatsapp"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
	wcuc "vozko/usecases/whatsapp_campaign"
)

var instanceID = uuid.NewString()

const (
	e2eRedisDB     = 12
	productionPool = 10
)

const seriesWorkflowID = "wf-serie"

type seriesWorkflow struct{ workspaceID string }

func (s seriesWorkflow) Execute(id string) (*workflow.Workflow, error) {
	if id != seriesWorkflowID {
		return nil, workflow.ErrWorkflowNotFound
	}
	return &workflow.Workflow{ID: id, WorkspaceID: s.workspaceID, Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "n1", Config: map[string]interface{}{"text": "Série {{campvars.serie}}"}},
	}}}, nil
}

type noAgents struct{}

func (noAgents) Execute(string) (*agent.Agent, error) { return nil, agent.ErrAgentNotFound }

type noExitRisk struct{}

func (noExitRisk) AdmitCampaign(context.Context, string, *template.Template, string) (buttonaction.CampaignAdmission, error) {
	return buttonaction.CampaignAdmission{}, nil
}

func (noExitRisk) RecordCampaignAdmission(context.Context, string, string, buttonaction.CampaignAdmission) error {
	return nil
}

type openPermissions struct{}

func (openPermissions) HasWorkspacePermission(string, string, string, string, bool) bool { return true }

type utcZones struct{}

func (utcZones) Location(context.Context, string, string) (*time.Location, error) {
	return time.UTC, nil
}

func (w *world) leadSelections(definitions lead_usecase.DefinitionSource) *lead_usecase.SelectionResolver {
	resolver, err := lead_usecase.NewSelectionResolver(lead_usecase.SelectionDeps{Reader: w.leads, Snapshots: w.leads, Permissions: openPermissions{}, Definitions: definitions, Zones: utcZones{}})
	w.must(err)
	return resolver
}

type everyScope struct{}

func (everyScope) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}

type connectedNumber struct{ workspaceID string }

func (g connectedNumber) Instance(_ context.Context, id string) (*uw.Instance, error) {
	if id != instanceID {
		return nil, uw.ErrInstanceNotFound
	}
	return &uw.Instance{ID: id, WorkspaceID: g.workspaceID, Status: uw.StatusConnected}, nil
}

func (connectedNumber) Ref(context.Context, *uw.Instance) (uw.InstanceRef, error) {
	return uw.InstanceRef{}, nil
}

func (connectedNumber) CheckNumbers(context.Context, uw.InstanceRef, []string) ([]uw.NumberCheck, error) {
	return nil, nil
}

func (connectedNumber) MessagingLimits(context.Context, uw.InstanceRef) (*uw.Restriction, error) {
	return nil, nil
}

func (connectedNumber) CacheRestriction(context.Context, string, uw.Restriction) error { return nil }

func (connectedNumber) Resolve(context.Context, *uw.Instance, uwuc.ResolveInput) (*uwuc.Resolved, error) {
	return nil, errors.New("not used")
}

type openGate struct{}

func (openGate) Acquire(context.Context) (func(), error) { return func() {}, nil }

type world struct {
	t          *testing.T
	ctx        context.Context
	db         *gorm.DB
	shared     cache.SharedState
	ws         string
	actor      campaignaudience_usecase.Actor
	templateID string
	phoneID    string
	leads      lead_repository.Repository
	audience   *campaignaudience_usecase.Service
	official   wc.CreateCampaignUseCase
	unofficial uwc.CreateCampaignUseCase
	entries    wce.Repository
	uEntries   uwc.EntryRepository
	waves      *wave_repository.Repository
}

func e2eDB(t *testing.T) *gorm.DB {
	t.Helper()
	testenv.Postgres(t)
	testenv.Redis(t)
	repotest.WithPII(t)
	db := repotest.IsolatedDB(t, "audience_e2e")
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(productionPool)
	pool.SetMaxIdleConns(productionPool)
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	models := []any{
		&schema.Lead{}, &schema.LeadEvent{}, &schema.LeadPhone{}, &schema.LeadAddress{}, &schema.LeadRelation{}, &schema.LeadCompany{},
		&schema.WhatsAppCampaign{}, &schema.WhatsAppCampaignEntry{}, &schema.WhatsAppTemplate{}, &schema.WhatsAppTemplateVersion{}, &schema.WhatsAppBusinessPhoneNumber{},
		&schema.WorkspaceTemplateAccess{}, &schema.UnofficialWhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaignEntry{},
		&schema.LeadCampaignSend{}, &schema.WorkspaceConfig{}, &schema.CustomFieldDefinition{}, &schema.WhatsAppCampaignWavePolicy{},
	}
	models = append(models, schema.CampaignAudienceModels()...)
	models = append(models, repotest.LeadStoreModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.CreateLeadCollectionConstraints(db); err != nil {
		t.Fatalf("constraints: %v", err)
	}
	if err := database.CreateSearchFold(db); err != nil {
		t.Fatalf("search fold: %v", err)
	}
	return db
}

func e2eRedis(t *testing.T) cache.SharedState {
	t.Helper()
	target := testenv.Redis(t)
	provider := infracache.NewRedisProviderOnDB(target.Addr, target.Password, e2eRedisDB)
	return provider.SharedState()
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := e2eDB(t)
	w := &world{t: t, ctx: context.Background(), db: db, shared: e2eRedis(t), ws: uuid.NewString(), templateID: uuid.NewString(), phoneID: uuid.NewString()}
	w.actor = campaignaudience_usecase.Actor{WorkspaceID: w.ws, UserID: uuid.NewString()}
	w.leads = lead_repository.NewRepository(db)
	templates := whatsapp_repository.NewTemplateRepository(db)
	w.must(templates.Create(&template.Template{ID: w.templateID, ExternalID: "ext-" + w.templateID, WABAId: "waba-1", Name: "matriculas", Language: "pt_BR",
		Category: template.TemplateCategoryMarketing, Status: template.TemplateStatusApproved, Components: []template.TemplateComponent{
			{Type: "BODY", Text: "Olá {{1}}, série {{2}}", Example: &template.TemplateExample{BodyText: [][]string{{"Maria", "1º ano"}}}},
		}}))
	phones := whatsapp_repository.NewBusinessPhoneRepository(db)
	w.must(phones.Create(&businessphone.WhatsAppBusinessPhoneNumber{ID: w.phoneID, MetaPhoneNumberID: "meta-" + w.phoneID, WABAId: "waba-1", OwnerWorkspaceID: w.ws, OwnerAssignedBy: w.actor.UserID, Status: businessphone.StatusConnected}))
	access := template_access_repository.NewRepository(db)
	w.must(access.Create(workspace_template_access.NewWorkspaceTemplateAccess(uuid.NewString(), w.ws, w.templateID, w.actor.UserID)))
	configs := workspace_config_repository.NewRepository(db)
	w.must(configs.EnsureExists(w.ctx, w.ws))
	policy, err := campaignguard.NewConfigSpamPolicy(configs)
	w.must(err)
	spam, err := campaignguard.NewSpamGuard(campaignguard.NewMemoSpamPolicy(policy, campaignguard.WorkspaceMemoTTL, nil), lead_campaign_send_repository.NewRepository(db), w.shared, nil)
	w.must(err)
	eligibility, err := campaignguard.NewEligibility(lead_repository.NewSendFacts(db), spam)
	w.must(err)
	definitions := customfield_repository.NewRepository(db)
	module, err := campaignaudiencewiring.New(db, campaignaudiencewiring.Adapters{Selections: w.leadSelections(definitions), Leads: w.leads, Definitions: definitions, Templates: workspaceTemplates{templates: templates, access: access}, Gate: openGate{}, Shared: w.shared, EmailsSeen: &hygienetest.SeenEmails{}})
	w.must(err)
	w.audience = module.Service
	step, err := campaignautomation.NewStep(seriesWorkflow{workspaceID: w.ws}, noAgents{})
	w.must(err)
	deps := campaignaudience_usecase.WhatsAppDeps{
		Screener:    eligibility,
		Subjects:    campaigncreate.SubjectSource{Leads: w.leads, Contacts: w.leads, Names: noNames{}},
		Definitions: definitions, Templates: workspaceTemplates{templates: templates, access: access}, Suggestions: module.Suggestions,
		Instances: uwcuc.NewCampaignInstanceUseCase(connectedNumber{workspaceID: w.ws}), Scopes: everyScope{}, Automation: step,
	}
	official, err := campaignaudience_usecase.NewOfficialChannel(deps)
	w.must(err)
	unofficial, err := campaignaudience_usecase.NewUnofficialChannel(deps)
	w.must(err)
	w.audience.AddChannel(official)
	w.audience.AddChannel(unofficial)

	campaigns := wc_repository.NewRepository(db)
	w.entries = wce_repository.NewRepository(db)
	create := wcuc.NewCreateCampaignUseCase(campaigns, w.entries, w.leads, templates, phones, nil, eligibility)
	configurable := create.(interface {
		SetTemplateGrants(wcuc.TemplateGrants)
		SetAutomation(wcuc.AutomationCheck)
		SetIdempotentCampaigns(wcuc.IdempotentCampaigns)
		SetExitRiskGate(wcuc.ExitRiskGate)
	})
	configurable.SetTemplateGrants(access)
	configurable.SetAutomation(step)
	configurable.SetIdempotentCampaigns(campaigns.(wcuc.IdempotentCampaigns))
	configurable.SetExitRiskGate(noExitRisk{})
	w.waves = wave_repository.NewRepository(db)
	w.official = smartcampaign_usecase.CreateWithPolicy(campaignaudience_usecase.OfficialCreate(create, w.audience), wavePolicies{w.waves}, nil)

	uCampaigns := uwc_repository.NewRepository(db)
	w.uEntries = uwc_repository.NewEntryRepository(db)
	uCreate := uwcuc.NewCreateCampaignUseCase(uCampaigns, w.uEntries, w.leads, connectedNumber{workspaceID: w.ws}, eligibility, nil)
	uConfigurable := uCreate.(interface {
		SetAutomation(uwcuc.AutomationCheck)
		SetLeadsByID(uwcuc.LeadsByID)
		SetIdempotentCampaigns(uwcuc.IdempotentCampaigns)
	})
	uConfigurable.SetAutomation(step)
	uConfigurable.SetLeadsByID(w.leads)
	uConfigurable.SetIdempotentCampaigns(uCampaigns.(uwcuc.IdempotentCampaigns))
	w.unofficial = campaignaudience_usecase.UnofficialCreate(uCreate, w.audience)
	return w
}

type noNames struct{}

func (noNames) Names(...string) map[string]string { return map[string]string{} }

type workspaceTemplates struct {
	templates template.Repository
	access    workspace_template_access.Repository
}

func (w workspaceTemplates) Get(workspaceID, templateID string) (*template.Template, error) {
	granted, err := w.access.HasAccess(workspaceID, templateID)
	if err != nil {
		return nil, err
	}
	if !granted {
		return nil, template.ErrTemplateAccessDenied
	}
	return w.templates.FindByID(templateID)
}

type wavePolicies struct{ repo *wave_repository.Repository }

func (p wavePolicies) Save(target *wc.Campaign, policy smartcampaign.Policy) error {
	return p.repo.SavePolicy(target.ID, policy)
}

func (w *world) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) seedLead(name, number string, custom map[string]any, blocked bool) string {
	w.t.Helper()
	row := schema.Lead{ID: uuid.NewString(), WorkspaceID: w.ws, Name: name, Number: schema.OptionalText(number), Source: schema.OptionalText(lead.SourceImport), Blocked: blocked, Version: 1}
	if custom != nil {
		raw, err := json.Marshal(custom)
		w.must(err)
		row.CustomFields = raw
	}
	w.must(w.db.Create(&row).Error)
	return row.ID
}

func (w *world) leadCount() int64 {
	var n int64
	w.must(w.db.Raw("SELECT count(*) FROM leads WHERE workspace_id = ? AND deleted_at IS NULL", w.ws).Scan(&n).Error)
	return n
}

type entryView struct {
	LeadID    string
	Status    string
	Variables []string
	Metadata  map[string]any
	ErrorCode int
}

func (w *world) entriesOf(campaignID string) []entryView {
	w.t.Helper()
	items, err := w.entries.ListByCampaignID(campaignID)
	w.must(err)
	out := make([]entryView, 0, len(items))
	for _, e := range items {
		out = append(out, entryView{LeadID: e.LeadID, Status: string(e.Status), Variables: e.Variables, Metadata: e.Metadata, ErrorCode: e.ErrorCode})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LeadID < out[j].LeadID })
	return out
}

func (w *world) officialCampaign(name string, inputs []wc.PhoneInput, recipients *audience.Request, policy *smartcampaign.Policy) (*wc.Campaign, error) {
	return w.official.Execute(w.ctx, &wc.Campaign{
		WorkspaceID: w.ws, Name: name, TemplateID: w.templateID, BusinessPhoneID: w.phoneID, Status: wc.CampaignStatusStopped,
		PhoneInputs: inputs, Recipients: recipients, WavePolicy: policy,
	})
}

func everyone(count int) *selection.Selection {
	s := selection.Selection{Mode: selection.ModeEveryone, Filter: &crmfilter.Filter{}, ExpectedCount: count}
	s.Fingerprint = selection.Fingerprint(crmfilter.Filter{})
	return &s
}

func TestAudienceE2E_AnOfficialListMakesTheSameEntriesAsThePhoneNumbersPayload(t *testing.T) {
	w := newWorld(t)
	maria := w.seedLead("Maria Souza", "5511900010142", nil, false)
	csv := "telefone;nome;var1;var2;Série\n(11) 90001-0142;Maria;M;1º;A\n11900023301;João;J;2º;\n123;Errado;x;y;z\n"
	preview, err := w.audience.Upload(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Upload{FileName: "lista.csv", Data: []byte(csv)}, campaignaudience_usecase.View{Params: campaignaudience_usecase.Params{TemplateID: w.templateID}})
	w.must(err)
	if preview.Rows != 2 || preview.Existing != 1 || preview.New != 1 || preview.Skipped.ByReason[campaign.ListInvalidNumber] != 1 || w.leadCount() != 1 {
		t.Fatalf("preview = %+v, leads %d", preview, w.leadCount())
	}
	count, err := w.audience.Count(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Params{TemplateID: w.templateID, BusinessPhoneID: w.phoneID}, audience.Spec{Source: audience.SourceList, ListID: preview.ID})
	w.must(err)
	if count.Sendable == nil || *count.Sendable != 2 || len(count.Blockers) != 0 {
		t.Fatalf("count = %+v", count)
	}
	open := []working_hours.Window{{Start: "08:00", End: "20:00"}}
	policy := smartcampaign.PresetPolicy(smartcampaign.Standard, &working_hours.Spec{Timezone: "America/Sao_Paulo", Days: map[string][]working_hours.Window{"mon": open, "tue": open, "wed": open, "thu": open, "fri": open}})
	fromList, err := w.officialCampaign("Lista", nil, &audience.Request{Spec: audience.Spec{Source: audience.SourceList, ListID: preview.ID}, UserID: w.actor.UserID}, &policy)
	w.must(err)
	if saved, err := w.waves.FindPolicy(fromList.ID); err != nil || saved == nil || saved.Preset != smartcampaign.Standard {
		t.Fatalf("the wave policy of a list campaign = %+v, %v", saved, err)
	}
	fromPayload, err := w.officialCampaign("Payload", []wc.PhoneInput{
		{Number: "5511900010142", Name: "Maria", Variables: []string{"M", "1º"}, Metadata: map[string]any{"serie": "A"}},
		{Number: "5511900023301", Name: "João", Variables: []string{"J", "2º"}},
	}, nil, nil)
	w.must(err)
	listEntries, payloadEntries := w.entriesOf(fromList.ID), w.entriesOf(fromPayload.ID)
	if len(listEntries) != 2 || !reflect.DeepEqual(listEntries, payloadEntries) {
		t.Fatalf("list entries %+v\npayload entries %+v", listEntries, payloadEntries)
	}
	if w.leadCount() != 2 {
		t.Fatalf("João was not created once: %d leads", w.leadCount())
	}
	var mariaEntry *entryView
	for i := range listEntries {
		if listEntries[i].LeadID == maria {
			mariaEntry = &listEntries[i]
		}
	}
	if mariaEntry == nil || mariaEntry.Status != string(wce.SendStatusPending) {
		t.Fatalf("Maria was not matched to her lead: %+v", listEntries)
	}
}

func TestAudienceE2E_OfficialLeadsBindVariablesAndRequireAChoice(t *testing.T) {
	w := newWorld(t)
	w.must(customfield_repository.NewRepository(w.db).Create(&customfield.Definition{ID: uuid.NewString(), WorkspaceID: w.ws, ObjectType: customfield.ObjectLead, Key: "serie", Label: "Série", Type: customfield.TypeText}))
	ana := w.seedLead("Ana Lima", "5511900000001", map[string]any{"serie": "1º ano"}, false)
	bia := w.seedLead("Bia Reis", "5511900000002", nil, false)
	caio := w.seedLead("Caio Melo", "5511900000003", map[string]any{"serie": "3º ano"}, true)
	_, err := w.audience.SaveTemplateSuggestions(w.ctx, w.actor, w.templateID, []audience.Suggestion{{Slot: 1, Source: campaign.BindFirstName}, {Slot: 2, Source: campaign.CustomBinding("serie")}})
	w.must(err)

	spec := audience.Spec{Source: audience.SourceLeads, Selection: everyone(3)}
	count, err := w.audience.Count(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Params{TemplateID: w.templateID}, spec)
	w.must(err)
	if count.Sendable != nil || !reflect.DeepEqual(count.Blockers, []audience.Blocker{audience.BlockerChoiceRequired}) || count.Variables[1].Missing != 1 || !count.Variables[0].Suggested || count.Out[string(campaign.SkipBlocked)] != 1 {
		t.Fatalf("count = %+v", count)
	}
	if _, err := w.officialCampaign("Sem escolha", nil, &audience.Request{Spec: spec, UserID: w.actor.UserID}, nil); !errors.Is(err, audience.ErrChoiceRequired) {
		t.Fatalf("create without a choice = %v", err)
	}

	spec.Variables = []audience.Variable{{Key: "2", Missing: audience.MissingExclude}}
	excluded, err := w.officialCampaign("Deixar de fora", nil, &audience.Request{Spec: spec, UserID: w.actor.UserID}, nil)
	w.must(err)
	byLead := map[string]entryView{}
	for _, e := range w.entriesOf(excluded.ID) {
		byLead[e.LeadID] = e
	}
	if e := byLead[ana]; e.Status != string(wce.SendStatusPending) || !reflect.DeepEqual(e.Variables, []string{"Ana", "1º ano"}) {
		t.Fatalf("Ana = %+v", e)
	}
	if e := byLead[bia]; e.ErrorCode != campaign.SkipMissingVariable.FailureCode() {
		t.Fatalf("Bia = %+v", e)
	}
	if e := byLead[caio]; e.ErrorCode != campaign.SkipBlocked.FailureCode() {
		t.Fatalf("Caio = %+v", e)
	}

	spec.Variables = []audience.Variable{{Key: "2", Missing: audience.MissingText, Text: "a definir"}}
	texted, err := w.officialCampaign("Texto padrão", nil, &audience.Request{Spec: spec, UserID: w.actor.UserID}, nil)
	w.must(err)
	for _, e := range w.entriesOf(texted.ID) {
		if e.LeadID == bia && (e.Status != string(wce.SendStatusPending) || !reflect.DeepEqual(e.Variables, []string{"Bia", "a definir"})) {
			t.Fatalf("Bia with a default text = %+v", e)
		}
	}

	stale := audience.Spec{Source: audience.SourceLeads, Selection: everyone(2), Variables: spec.Variables}
	var changed *selection.CountChangedError
	if _, err := w.officialCampaign("Contagem velha", nil, &audience.Request{Spec: stale, UserID: w.actor.UserID}, nil); !errors.As(err, &changed) {
		t.Fatalf("a stale count = %v", err)
	}
}

func TestAudienceE2E_AnOfficialListFillsEmptySuggestedCellsFromTheLead(t *testing.T) {
	w := newWorld(t)
	w.seedLead("Maria Souza", "5511900010142", nil, false)
	_, err := w.audience.SaveTemplateSuggestions(w.ctx, w.actor, w.templateID, []audience.Suggestion{{Slot: 1, Source: campaign.BindFirstName}})
	w.must(err)
	sample, err := w.audience.Sample(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Params{TemplateID: w.templateID}, nil, campaignaudience_usecase.SampleCSV)
	w.must(err)
	if got := string(sample.Data); got[:len("number;name;primeiro_nome;var2")] != "number;name;primeiro_nome;var2" {
		t.Fatalf("sample = %q", got)
	}
	csv := "telefone;nome;primeiro_nome;var2\n5511900010142;;;1º\n5511900099901;Rita Alves;;2º\n5511900099902;;;3º\n"
	preview, err := w.audience.Upload(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Upload{Data: []byte(csv)}, campaignaudience_usecase.View{Params: campaignaudience_usecase.Params{TemplateID: w.templateID}})
	w.must(err)
	if v := preview.Columns.Variables; v[0].Column != "primeiro_nome" || v[0].Field != campaign.BindFirstName || v[1].Column != "var2" {
		t.Fatalf("mapping = %+v", v)
	}
	spec := audience.Spec{Source: audience.SourceList, ListID: preview.ID}
	if _, err := w.officialCampaign("Faltando", nil, &audience.Request{Spec: spec, UserID: w.actor.UserID}, nil); !errors.Is(err, audience.ErrListVariablesMissing) {
		t.Fatalf("a row without a first name anywhere = %v", err)
	}
	page, err := w.audience.Rows(w.ctx, w.actor, audience.ChannelWhatsApp, preview.ID, campaignaudience_usecase.RowsQuery{View: campaignaudience_usecase.View{Params: campaignaudience_usecase.Params{TemplateID: w.templateID}}, MissingOnly: true, Page: 1, Limit: 50})
	w.must(err)
	if page.Total != 1 || page.Items[0].Contact != "5511900099902" {
		t.Fatalf("missing rows = %+v", page)
	}
	w.must(w.audience.RemoveRow(w.ctx, w.actor, audience.ChannelWhatsApp, preview.ID, page.Items[0].ID))
	created, err := w.officialCampaign("Preenchida", nil, &audience.Request{Spec: spec, UserID: w.actor.UserID}, nil)
	w.must(err)
	values := map[string][]string{}
	for _, e := range w.entriesOf(created.ID) {
		values[e.Variables[1]] = e.Variables
	}
	if !reflect.DeepEqual(values["1º"], []string{"Maria", "1º"}) || !reflect.DeepEqual(values["2º"], []string{"Rita", "2º"}) {
		t.Fatalf("filled = %+v", values)
	}
}

func TestAudienceE2E_UnofficialListAndLeadsCreateTargetsButNeverGroups(t *testing.T) {
	w := newWorld(t)
	w.seedLead("Ana Lima", "5511900000001", nil, false)
	message := uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oi {{1}}! Responda /sair para não receber mais mensagens."}}
	preview, err := w.audience.Upload(w.ctx, w.actor, audience.ChannelUnofficial, campaignaudience_usecase.Upload{Data: []byte("numero;nome;variavel1\n5511900010142;Maria;Maria\n")}, campaignaudience_usecase.View{Params: campaignaudience_usecase.Params{Message: &message}})
	w.must(err)
	if preview.Rows != 1 || preview.Skipped.Total != 0 || !preview.Header {
		t.Fatalf("the sample header is read as a header: %+v", preview)
	}
	base := uwc.Campaign{WorkspaceID: w.ws, InstanceID: instanceID, Name: "Rematrícula", Message: message, Status: campaign.StatusStopped}
	fromList := base
	fromList.Recipients = &audience.Request{Spec: audience.Spec{Source: audience.SourceList, ListID: preview.ID}, UserID: w.actor.UserID}
	created, err := w.unofficial.Execute(w.ctx, &fromList, uw.DepartmentScope{})
	w.must(err)
	entries, err := w.uEntries.ListByStatus(created.ID, campaign.SendStatusPending, 10)
	w.must(err)
	if len(entries) != 1 || entries[0].Number != "5511900010142" || !reflect.DeepEqual(entries[0].Variables, []string{"Maria"}) {
		t.Fatalf("list entries = %+v", entries)
	}

	fromLeads := base
	fromLeads.Name = "Leads"
	fromLeads.Recipients = &audience.Request{Spec: audience.Spec{Source: audience.SourceLeads, Selection: everyone(2), Variables: []audience.Variable{{Key: "1", Source: campaign.BindFirstName, Missing: audience.MissingText, Text: "família"}}}, UserID: w.actor.UserID}
	created, err = w.unofficial.Execute(w.ctx, &fromLeads, uw.DepartmentScope{})
	w.must(err)
	entries, err = w.uEntries.ListByStatus(created.ID, campaign.SendStatusPending, 10)
	w.must(err)
	if len(entries) != 2 {
		t.Fatalf("lead entries = %+v", entries)
	}

	groups := base
	groups.Audience = uwc.AudienceGroups
	groups.Recipients = &audience.Request{Spec: audience.Spec{Source: audience.SourceList, ListID: preview.ID}, UserID: w.actor.UserID}
	if _, err := w.unofficial.Execute(w.ctx, &groups, uw.DepartmentScope{}); !errors.Is(err, audience.ErrSourceUnsupported) {
		t.Fatalf("groups = %v", err)
	}
}

func TestAudienceE2E_AnExpiredOrForeignListIsRefusedAtCreate(t *testing.T) {
	w := newWorld(t)
	preview, err := w.audience.Upload(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Upload{Data: []byte("telefone;var1;var2\n5511900010142;a;b\n")}, campaignaudience_usecase.View{})
	w.must(err)
	other := &audience.Request{Spec: audience.Spec{Source: audience.SourceList, ListID: preview.ID}, UserID: uuid.NewString()}
	if _, err := w.officialCampaign("Outro", nil, other, nil); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("another member = %v", err)
	}
	w.must(w.db.Exec("UPDATE campaign_audience_lists SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Minute), preview.ID).Error)
	mine := &audience.Request{Spec: audience.Spec{Source: audience.SourceList, ListID: preview.ID}, UserID: w.actor.UserID}
	if _, err := w.officialCampaign("Vencida", nil, mine, nil); !errors.Is(err, audience.ErrListExpired) {
		t.Fatalf("expired = %v", err)
	}
	n, err := w.audience.Sweep(w.ctx)
	if err != nil || n < 1 {
		t.Fatalf("sweep = %d %v", n, err)
	}
}

func TestAudienceE2E_RowsMissingTheWorkflowMetadataAreReportedAndRefusedLikeThePayload(t *testing.T) {
	w := newWorld(t)
	w.seedLead("Ana Lima", "5511900000001", nil, false)
	automation := campaign.Automation{WorkflowID: seriesWorkflowID, EnableWorkflow: true}
	params := campaignaudience_usecase.Params{TemplateID: w.templateID, BusinessPhoneID: w.phoneID, Automation: automation}
	csv := "telefone;nome;var1;var2;Série\n(11) 90001-0142;Maria;M;1º;A\n11900023301;João;J;2º;\n"
	preview, err := w.audience.Upload(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Upload{FileName: "lista.csv", Data: []byte(csv)}, campaignaudience_usecase.View{Params: params})
	w.must(err)
	want := []audience.MetadataGap{{Key: "serie", Missing: 1, Lines: []int{3}}}
	if !reflect.DeepEqual(preview.MetadataMissing, want) {
		t.Fatalf("preview metadata = %+v", preview.MetadataMissing)
	}
	count, err := w.audience.Count(w.ctx, w.actor, audience.ChannelWhatsApp, params, audience.Spec{Source: audience.SourceList, ListID: preview.ID})
	w.must(err)
	if !reflect.DeepEqual(count.MetadataMissing, want) || !reflect.DeepEqual(count.Blockers, []audience.Blocker{audience.BlockerListMetadataMissing}) {
		t.Fatalf("count = %+v", count)
	}
	_, err = w.official.Execute(w.ctx, &wc.Campaign{
		WorkspaceID: w.ws, Name: "Lista", TemplateID: w.templateID, BusinessPhoneID: w.phoneID, Status: wc.CampaignStatusStopped,
		WorkflowID: automation.WorkflowID, EnableWorkflow: true, Recipients: &audience.Request{Spec: audience.Spec{Source: audience.SourceList, ListID: preview.ID}, UserID: w.actor.UserID},
	})
	if !errors.Is(err, audience.ErrListMetadataMissing) {
		t.Fatalf("list create = %v", err)
	}
	_, err = w.official.Execute(w.ctx, &wc.Campaign{
		WorkspaceID: w.ws, Name: "Payload", TemplateID: w.templateID, BusinessPhoneID: w.phoneID, Status: wc.CampaignStatusStopped,
		WorkflowID: automation.WorkflowID, EnableWorkflow: true, PhoneInputs: []wc.PhoneInput{
			{Number: "5511900010142", Name: "Maria", Variables: []string{"M", "1º"}, Metadata: map[string]any{"serie": "A"}},
			{Number: "5511900023301", Name: "João", Variables: []string{"J", "2º"}},
		},
	})
	if !errors.Is(err, campaign.ErrWorkflowVarsMissing) {
		t.Fatalf("the same rows by payload = %v", err)
	}
	leads, err := w.audience.Count(w.ctx, w.actor, audience.ChannelWhatsApp, params, audience.Spec{Source: audience.SourceLeads, Selection: everyone(1), Variables: []audience.Variable{{Key: "1", Source: campaign.BindFirstName, Missing: audience.MissingText, Text: "cliente"}, {Key: "2", Source: campaign.BindLiteral, Value: "1º"}}})
	w.must(err)
	if !reflect.DeepEqual(leads.Blockers, []audience.Blocker{audience.BlockerLeadsMetadataUnavailable}) {
		t.Fatalf("leads count = %+v", leads)
	}
}

func (w *world) seedTemplate(name string, format template.ParameterFormat, components ...template.TemplateComponent) *template.Template {
	w.t.Helper()
	tmpl := &template.Template{ID: uuid.NewString(), WABAId: "waba-1", Name: name, Language: "pt_BR", Category: template.TemplateCategoryMarketing,
		Status: template.TemplateStatusApproved, ParameterFormat: format, Components: components}
	tmpl.ExternalID = "ext-" + tmpl.ID
	w.must(whatsapp_repository.NewTemplateRepository(w.db).Create(tmpl))
	w.must(template_access_repository.NewRepository(w.db).Create(workspace_template_access.NewWorkspaceTemplateAccess(uuid.NewString(), w.ws, tmpl.ID, w.actor.UserID)))
	return tmpl
}

func (w *world) campaignFrom(templateID, name string, recipients *audience.Request) []entryView {
	w.t.Helper()
	created, err := w.official.Execute(w.ctx, &wc.Campaign{WorkspaceID: w.ws, Name: name, TemplateID: templateID, BusinessPhoneID: w.phoneID, Status: wc.CampaignStatusStopped, Recipients: recipients})
	w.must(err)
	return w.entriesOf(created.ID)
}

func TestAudienceE2E_LeadsTakeNamedAndHeaderTemplatesLikeAList(t *testing.T) {
	w := newWorld(t)
	ana := w.seedLead("Ana Lima", "5511900000001", nil, false)
	w.seedLead("Caio Melo", "5511900000003", nil, true)

	pending, err := w.audience.Count(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Params{BusinessPhoneID: w.phoneID}, audience.Spec{Source: audience.SourceLeads, Selection: everyone(2)})
	w.must(err)
	if pending.Matched != 2 || pending.Sendable == nil || *pending.Sendable != 1 || pending.Out[string(campaign.SkipBlocked)] != 1 || len(pending.Variables) != 0 || !reflect.DeepEqual(pending.Blockers, []audience.Blocker{audience.BlockerTemplateRequired}) {
		t.Fatalf("count before the template = %+v", pending)
	}

	named := w.seedTemplate("notificacao_compra", template.ParameterFormatNamed,
		template.TemplateComponent{Type: "HEADER", Format: "TEXT", Text: "Pedido {{loja}}"},
		template.TemplateComponent{Type: "BODY", Text: "Olá {{nome}}, sua compra {{pedido}} saiu"})
	headed := w.seedTemplate("aviso_loja", template.ParameterFormatPositional,
		template.TemplateComponent{Type: "BODY", Text: "Olá {{1}}, chegou"},
		template.TemplateComponent{Type: "HEADER", Format: "TEXT", Text: "Loja {{1}}"})
	_, err = w.audience.SaveTemplateSuggestions(w.ctx, w.actor, named.ID, []audience.Suggestion{{Slot: 2, Source: campaign.BindFirstName}})
	w.must(err)

	cases := []struct {
		tmpl      *template.Template
		variables []audience.Variable
		names     []string
		values    []string
		header    []string
		body      []string
		csv       string
	}{
		{tmpl: named, variables: []audience.Variable{{Key: "1", Source: campaign.BindLiteral, Value: "Prisma"}, {Key: "3", Source: campaign.BindLiteral, Value: "77"}},
			names: []string{"loja", "nome", "pedido"}, values: []string{"Prisma", "Ana", "77"}, header: []string{"Prisma"}, body: []string{"Ana", "77"},
			csv: "telefone;nome;var1;var2;var3\n5511900000001;Ana;Prisma;Ana;77\n"},
		{tmpl: headed, variables: []audience.Variable{{Key: "1", Source: campaign.BindLiteral, Value: "Prisma"}, {Key: "2", Source: campaign.BindFirstName}},
			names: []string{"1", "1"}, values: []string{"Prisma", "Ana"}, header: []string{"Prisma"}, body: []string{"Ana"},
			csv: "telefone;nome;var1;var2\n5511900000001;Ana;Prisma;Ana\n"},
	}
	for _, tc := range cases {
		params := campaignaudience_usecase.Params{TemplateID: tc.tmpl.ID, BusinessPhoneID: w.phoneID}
		spec := audience.Spec{Source: audience.SourceLeads, Selection: everyone(2), Variables: tc.variables}
		count, err := w.audience.Count(w.ctx, w.actor, audience.ChannelWhatsApp, params, spec)
		w.must(err)
		if len(count.Blockers) != 0 || count.Sendable == nil || *count.Sendable != 1 || len(count.Variables) != len(tc.names) || !count.Variables[0].Header || count.Variables[1].Header {
			t.Fatalf("%s count = %+v", tc.tmpl.Name, count)
		}
		for i, name := range tc.names {
			if count.Variables[i].Name != name {
				t.Fatalf("%s variable %d = %+v", tc.tmpl.Name, i, count.Variables[i])
			}
		}
		fromLeads := w.campaignFrom(tc.tmpl.ID, "Leads "+tc.tmpl.Name, &audience.Request{Spec: spec, UserID: w.actor.UserID})
		var anaEntry entryView
		for _, e := range fromLeads {
			if e.LeadID == ana {
				anaEntry = e
			}
		}
		if anaEntry.Status != string(wce.SendStatusPending) || !reflect.DeepEqual(anaEntry.Variables, tc.values) {
			t.Fatalf("%s leads entry = %+v", tc.tmpl.Name, fromLeads)
		}
		preview, err := w.audience.Upload(w.ctx, w.actor, audience.ChannelWhatsApp, campaignaudience_usecase.Upload{FileName: "lista.csv", Data: []byte(tc.csv)}, campaignaudience_usecase.View{Params: params})
		w.must(err)
		fromList := w.campaignFrom(tc.tmpl.ID, "Lista "+tc.tmpl.Name, &audience.Request{Spec: audience.Spec{Source: audience.SourceList, ListID: preview.ID}, UserID: w.actor.UserID})
		if len(fromList) != 1 || !reflect.DeepEqual(fromList[0].Variables, anaEntry.Variables) {
			t.Fatalf("%s list entries %+v, leads entry %+v", tc.tmpl.Name, fromList, anaEntry)
		}
		body, header := tc.tmpl.SplitParameters(anaEntry.Variables)
		if !reflect.DeepEqual(header, tc.header) || !reflect.DeepEqual(body, tc.body) {
			t.Fatalf("%s sends header %q body %q", tc.tmpl.Name, header, body)
		}
	}
}

func (w *world) editLead(id string, change func(*lead.Lead)) {
	w.t.Helper()
	current, err := w.leads.Load(w.ctx, w.ws, id)
	w.must(err)
	change(current)
	w.must(w.leads.Save(w.ctx, current, current.Version, nil))
}

func (w *world) check(ch audience.Channel, params campaignaudience_usecase.Params, spec audience.Spec) audiencecheck.Audience {
	return audiencecheck.Audience{Service: w.audience, Actor: w.actor, Channel: ch, Params: params, Spec: spec}
}

func TestAudienceE2E_TheLeadsBehindEachNumberAreListedAndFixedByEditingTheLead(t *testing.T) {
	w := newWorld(t)
	w.must(customfield_repository.NewRepository(w.db).Create(&customfield.Definition{ID: uuid.NewString(), WorkspaceID: w.ws, ObjectType: customfield.ObjectLead, Key: "serie", Label: "Série", Type: customfield.TypeText}))
	ana := w.seedLead("Ana Lima", "5511900000001", map[string]any{"serie": "1º ano"}, false)
	bia := w.seedLead("Bia Melo", "5511900000002", nil, false)
	w.seedLead("Caio Reis", "5511900000003", nil, true)
	w.seedLead("Duda Sem Número", "", nil, false)

	official := campaignaudience_usecase.Params{TemplateID: w.templateID, BusinessPhoneID: w.phoneID}
	variables := []audience.Variable{{Key: "1", Source: campaign.BindFirstName, Missing: audience.MissingText, Text: "cliente"}, {Key: "2", Source: campaign.CustomBinding("serie"), Missing: audience.MissingExclude}}
	spec := audience.Spec{Source: audience.SourceLeads, Selection: everyone(4), Variables: variables}
	count := w.check(audience.ChannelWhatsApp, official, spec).RequireLeadsMatchTheCount(t)
	if count.Out[audience.ReasonMissingVariable] != 1 || count.Out[string(campaign.SkipBlocked)] != 1 || count.Out[string(campaign.SkipNoIdentity)] != 1 || *count.Sendable != 1 {
		t.Fatalf("official count = %+v", count)
	}
	lacking := w.check(audience.ChannelWhatsApp, official, spec).Leads(t, audience.LeadFilter{Variable: "2"})
	if lacking.Total != 1 || lacking.Items[0].LeadID != bia || lacking.Items[0].Reason != audience.ReasonMissingVariable || lacking.Items[0].Contact != "5511900000002" {
		t.Fatalf("the lead without a série = %+v", lacking)
	}

	message := uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oi {{1}}, série {{2}}. Responda /sair para sair."}}
	unofficial := campaignaudience_usecase.Params{Message: &message, InstanceID: instanceID}
	w.check(audience.ChannelUnofficial, unofficial, spec).RequireLeadsMatchTheCount(t)

	w.editLead(bia, func(l *lead.Lead) { l.CustomFields = map[string]any{"serie": "2º ano"} })
	fixed := w.check(audience.ChannelWhatsApp, official, spec).RequireLeadsMatchTheCount(t)
	if fixed.Out[audience.ReasonMissingVariable] != 0 || *fixed.Sendable != 2 {
		t.Fatalf("after the edit = %+v", fixed)
	}
	ready := w.check(audience.ChannelWhatsApp, official, spec).Leads(t, audience.LeadFilter{Status: audience.LeadReady, Search: "bia"})
	if ready.Total != 1 || !reflect.DeepEqual(ready.Items[0].Variables, []string{"Bia", "2º ano"}) {
		t.Fatalf("Bia after the edit = %+v", ready)
	}

	picked := audience.Spec{Source: audience.SourceLeads, Selection: &selection.Selection{Mode: selection.ModeIDs, IDs: []string{ana, bia}}, Variables: variables}
	narrowed := w.check(audience.ChannelWhatsApp, official, picked).RequireLeadsMatchTheCount(t)
	if narrowed.Matched != 2 || narrowed.Selection.ConfirmCount != 0 {
		t.Fatalf("picked count = %+v", narrowed)
	}
	entries := w.campaignFrom(w.templateID, "Escolhidos", &audience.Request{Spec: picked, UserID: w.actor.UserID})
	if len(entries) != 2 || entries[0].Status != string(wce.SendStatusPending) || entries[1].Status != string(wce.SendStatusPending) {
		t.Fatalf("picked entries = %+v", entries)
	}
}
