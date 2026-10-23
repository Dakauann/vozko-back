package campaignaudience_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/conversation"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
	"vozko/usecases/campaignautomation"
	"vozko/usecases/campaigncreate"
	"vozko/usecases/campaignguard"
)

type fakeScreener struct {
	skipped map[string]campaign.SkipReason
	running map[string]bool
	senders []string
}

func (f *fakeScreener) Check(context.Context, string, string, string) (campaign.SkipReason, error) {
	return "", nil
}

func (f *fakeScreener) Screen(_ context.Context, _ string, ids []string, sender string) (campaignguard.Screening, error) {
	f.senders = append(f.senders, sender)
	out := map[string]campaign.SkipReason{}
	for _, id := range ids {
		if r, ok := f.skipped[id]; ok {
			out[id] = r
		}
	}
	return campaignguard.Screening{Skipped: out, CooldownDays: 7}, nil
}

func (f *fakeScreener) ScreenWithRunning(ctx context.Context, ws string, ids []string, sender string) (campaignguard.Screening, error) {
	screening, err := f.Screen(ctx, ws, ids, sender)
	for _, id := range ids {
		if _, skipped := screening.Skipped[id]; !skipped && f.running[id] {
			screening.Skipped[id] = campaign.SkipAlreadyInRunningCampaign
		}
	}
	return screening, err
}

type leadBook map[string]*lead.Lead

func (b leadBook) BindingLeads(_ context.Context, _ string, ids []string) ([]*lead.Lead, error) {
	out := []*lead.Lead{}
	for _, id := range ids {
		if l, ok := b[id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

type noAddresses struct{}

func (noAddresses) AttachContactDetails(context.Context, string, []*lead.Lead) error { return nil }

type noNames struct{}

func (noNames) Names(...string) map[string]string { return map[string]string{} }

type fakeTemplates map[string]*template.Template

func (f fakeTemplates) Get(_ string, id string) (*template.Template, error) {
	if t, ok := f[id]; ok {
		return t, nil
	}
	return nil, template.ErrTemplateAccessDenied
}

type fakeInstances struct{ seen []uw.DepartmentScope }

func (f *fakeInstances) Usable(_ context.Context, _ string, scope uw.DepartmentScope, id string) (*uw.Instance, error) {
	f.seen = append(f.seen, scope)
	if id != "inst-1" {
		return nil, uw.ErrInstanceNotFound
	}
	return &uw.Instance{ID: id}, nil
}

type everyScope struct{}

func (everyScope) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}

func approvedTemplate(body string, examples ...string) *template.Template {
	return &template.Template{ID: "tpl-1", Status: template.TemplateStatusApproved, Components: []template.TemplateComponent{
		{Type: "BODY", Text: body, Example: &template.TemplateExample{BodyText: [][]string{examples}}},
	}}
}

type whatsappWorld struct {
	deps     WhatsAppDeps
	screener *fakeScreener
	official Channel
	unoff    Channel
	inst     *fakeInstances
	suggest  *memSuggestions
}

func newWhatsAppWorld(t *testing.T) *whatsappWorld {
	t.Helper()
	w := &whatsappWorld{screener: &fakeScreener{skipped: map[string]campaign.SkipReason{}, running: map[string]bool{"busy": true}}, inst: &fakeInstances{}, suggest: &memSuggestions{}}
	w.deps = WhatsAppDeps{
		Screener: w.screener,
		Subjects: campaigncreate.SubjectSource{Leads: leadBook{
			"ana":  {ID: "ana", WorkspaceID: "ws-1", Name: "Ana Souza", Number: "5511900000001"},
			"bia":  {ID: "bia", WorkspaceID: "ws-1", Name: "", Number: "5511900000002"},
			"busy": {ID: "busy", WorkspaceID: "ws-1", Name: "Caio", Number: "5511900000003"},
		}, Contacts: noAddresses{}, Names: noNames{}},
		Definitions: fakeDefinitions{{Key: "serie", ObjectType: customfield.ObjectLead, Type: customfield.TypeText}},
		Templates: fakeTemplates{
			"tpl-1":   approvedTemplate("Olá {{1}}, sua série {{2}}", "Maria", "1º ano"),
			"named":   {ID: "named", Status: template.TemplateStatusApproved, ParameterFormat: template.ParameterFormatNamed, Components: []template.TemplateComponent{{Type: "HEADER", Format: "TEXT", Text: "Loja {{loja}}"}, {Type: "BODY", Text: "Olá {{nome}}"}}},
			"pending": {ID: "pending", Status: template.TemplateStatusPending, Components: []template.TemplateComponent{{Type: "BODY", Text: "Olá"}}},
		},
		Suggestions: w.suggest,
		Instances:   w.inst,
		Scopes:      everyScope{},
		Automation:  classRules(t),
	}
	var err error
	if w.official, err = NewOfficialChannel(w.deps); err != nil {
		t.Fatal(err)
	}
	if w.unoff, err = NewUnofficialChannel(w.deps); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestOfficialChannelNumbersTheTemplateVariablesWithTheirSuggestionAndExample(t *testing.T) {
	w := newWhatsAppWorld(t)
	w.suggest.stored = map[string][]campaignaudience.Suggestion{"ws-1|tpl-1": {{Slot: 1, Source: campaign.BindFirstName}, {Slot: 2, Source: campaign.CustomBinding("removed")}}}
	prepared, err := w.official.Prepare(ctx, testActor, Params{TemplateID: "tpl-1", BusinessPhoneID: "phone-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []campaignaudience.Slot{{Key: "1", Name: "1", Suggested: campaign.BindFirstName, Example: "Maria"}, {Key: "2", Name: "2", Example: "1º ano"}}
	if !reflect.DeepEqual(prepared.Slots, want) {
		t.Fatalf("slots = %+v, want the stale custom field suggestion dropped", prepared.Slots)
	}
}

func TestOfficialChannelRefusesATemplateItCannotSend(t *testing.T) {
	w := newWhatsAppWorld(t)
	if _, err := w.official.Prepare(ctx, testActor, Params{TemplateID: "other"}); !errors.Is(err, wc.ErrCampaignTemplateNotFound) {
		t.Fatalf("foreign template = %v", err)
	}
	if _, err := w.official.Prepare(ctx, testActor, Params{TemplateID: "pending"}); !errors.Is(err, template.ErrTemplateNotSendable) {
		t.Fatalf("pending template = %v", err)
	}
}

func boundOf(t *testing.T, prepared Prepared, vars ...campaignaudience.Variable) []campaignaudience.Bound {
	t.Helper()
	bound, err := campaignaudience.Bind(prepared.Slots, vars)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

func TestOfficialLeadsAreScreenedLikeTheLeadsPageSend(t *testing.T) {
	w := newWhatsAppWorld(t)
	w.screener.skipped["bia"] = campaign.SkipCooldown
	w.screener.skipped["busy"] = campaign.SkipBlocked
	prepared, err := w.official.Prepare(ctx, testActor, Params{TemplateID: "tpl-1", BusinessPhoneID: "phone-1"})
	if err != nil {
		t.Fatal(err)
	}
	judge, err := prepared.Open(ctx, boundOf(t, prepared, campaignaudience.Variable{Key: "1", Source: campaign.BindFirstName}, campaignaudience.Variable{Key: "2", Source: campaign.BindLiteral, Value: "1º ano"}), campaignaudience.SourceLeads)
	if err != nil {
		t.Fatal(err)
	}
	got, err := judge.Leads(ctx, []string{"ana", "bia", "busy"})
	if err != nil {
		t.Fatal(err)
	}
	want := []campaignaudience.Candidate{
		{LeadID: "ana", Contact: "5511900000001", Name: "Ana Souza", HasContact: true, Values: []string{"Ana", "1º ano"}},
		{LeadID: "bia", Contact: "5511900000002", HasContact: true, Refusal: string(campaign.SkipCooldown), Values: []string{"", "1º ano"}},
		{LeadID: "busy", Contact: "5511900000003", Name: "Caio", HasContact: true, Refusal: string(campaign.SkipBlocked), Values: []string{"Caio", "1º ano"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %+v\nwant %+v", got, want)
	}
	if !reflect.DeepEqual(w.screener.senders, []string{"phone-1"}) {
		t.Fatalf("screened with sender %v", w.screener.senders)
	}
	w.screener.skipped = map[string]campaign.SkipReason{}
	again, err := judge.Leads(ctx, []string{"busy"})
	if err != nil || again[0].Refusal != string(campaign.SkipAlreadyInRunningCampaign) {
		t.Fatalf("a lead in a running campaign = %+v, %v", again, err)
	}
	if _, err := prepared.Open(ctx, boundOf(t, prepared, campaignaudience.Variable{Key: "1", Source: campaign.CustomBinding("cpf")}), campaignaudience.SourceLeads); !errors.Is(err, campaign.ErrBindingFieldUnknown) {
		t.Fatalf("an unknown field = %v", err)
	}
}

func TestOfficialListRowsFillEmptySuggestedCellsFromTheLeadOrTheRowName(t *testing.T) {
	w := newWhatsAppWorld(t)
	w.screener.skipped["busy"] = campaign.SkipOptedOut
	prepared, err := w.official.Prepare(ctx, testActor, Params{TemplateID: "tpl-1"})
	if err != nil {
		t.Fatal(err)
	}
	judge, err := prepared.Open(ctx, boundOf(t, prepared, campaignaudience.Variable{Key: "1", Source: campaign.BindFirstName}), campaignaudience.SourceList)
	if err != nil {
		t.Fatal(err)
	}
	rows := []RowCandidate{
		{Row: campaignaudience.Row{ListRow: campaign.ListRow{Contact: "5511900000001"}}, LeadID: "ana", Values: []string{"", "1º"}},
		{Row: campaignaudience.Row{ListRow: campaign.ListRow{Contact: "5511900000009", Name: "Rita Lima"}}, Values: []string{"", ""}},
		{Row: campaignaudience.Row{ListRow: campaign.ListRow{Contact: "5511900000001", Name: "Aninha Souza"}}, LeadID: "ana", Values: []string{"", "2º"}},
		{Row: campaignaudience.Row{ListRow: campaign.ListRow{Contact: "5511900000003"}}, LeadID: "busy", Values: []string{"Kaio", "3º"}},
		{Row: campaignaudience.Row{ListRow: campaign.ListRow{Contact: "5511900000008"}}, Values: []string{"", "4º"}},
	}
	got, err := judge.Rows(ctx, rows)
	if err != nil {
		t.Fatal(err)
	}
	values := [][]string{}
	refusals := []string{}
	for _, c := range got {
		values = append(values, c.Values)
		refusals = append(refusals, c.Refusal)
	}
	wantValues := [][]string{{"Ana", "1º"}, {"Rita", ""}, {"Aninha", "2º"}, {"Kaio", "3º"}, {"", "4º"}}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("values = %q\nwant %q", values, wantValues)
	}
	if !reflect.DeepEqual(refusals, []string{"", "", "", string(campaign.SkipOptedOut), ""}) {
		t.Fatalf("refusals = %q", refusals)
	}
	if got[1].Contact != "5511900000009" || got[1].Name != "Rita Lima" || !got[1].HasContact {
		t.Fatalf("row candidate = %+v", got[1])
	}
}

func TestUnofficialChannelCountsTheMessageVariablesAndChecksTheNumber(t *testing.T) {
	w := newWhatsAppWorld(t)
	message := &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oi {{1}}, {{2}}"}}
	prepared, err := w.unoff.Prepare(ctx, testActor, Params{Message: message})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.Slots, []campaignaudience.Slot{{Key: "1"}, {Key: "2"}}) || len(w.inst.seen) != 0 {
		t.Fatalf("slots = %+v seen %v", prepared.Slots, w.inst.seen)
	}
	if _, err := w.unoff.Prepare(ctx, testActor, Params{Message: message, InstanceID: "inst-2"}); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("another number = %v", err)
	}
	prepared, err = w.unoff.Prepare(ctx, testActor, Params{Message: message, InstanceID: "inst-1"})
	if err != nil {
		t.Fatal(err)
	}
	judge, err := prepared.Open(ctx, boundOf(t, prepared, campaignaudience.Variable{Key: "1", Source: campaign.BindFirstName}, campaignaudience.Variable{Key: "2", Source: campaign.BindLiteral, Value: "x"}), campaignaudience.SourceLeads)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := judge.Leads(ctx, []string{"ana"}); err != nil || w.screener.senders[len(w.screener.senders)-1] != "inst-1" {
		t.Fatalf("screened with %v, %v", w.screener.senders, err)
	}
}

func TestWhatsAppChannelsFailClosedWithoutTheirDependencies(t *testing.T) {
	if _, err := NewOfficialChannel(WhatsAppDeps{}); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("official = %v", err)
	}
	if _, err := NewUnofficialChannel(WhatsAppDeps{}); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("unofficial = %v", err)
	}
}

type fakeWorkflowBook map[string]*workflow.Workflow

func (f fakeWorkflowBook) Execute(id string) (*workflow.Workflow, error) {
	if wf, ok := f[id]; ok {
		return wf, nil
	}
	return nil, workflow.ErrWorkflowNotFound
}

type noAgents struct{}

func (noAgents) Execute(string) (*agent.Agent, error) { return nil, agent.ErrAgentNotFound }

func classRules(t *testing.T) campaignautomation.Rules {
	t.Helper()
	step, err := campaignautomation.NewStep(fakeWorkflowBook{"wf-1": {ID: "wf-1", WorkspaceID: "ws-1", Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "n1", Config: map[string]interface{}{"text": "Turma {{campvars.turma}}"}},
	}}}}, noAgents{})
	if err != nil {
		t.Fatal(err)
	}
	return step
}

func TestWhatsAppChannelsJudgeMetadataWithTheCreateAutomationRule(t *testing.T) {
	w := newWhatsAppWorld(t)
	message := &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oi"}}
	for name, prepare := range map[string]func(Params) (Prepared, error){
		"official":   func(p Params) (Prepared, error) { p.TemplateID = "tpl-1"; return w.official.Prepare(ctx, testActor, p) },
		"unofficial": func(p Params) (Prepared, error) { p.Message = message; return w.unoff.Prepare(ctx, testActor, p) },
	} {
		prepared, err := prepare(Params{Automation: campaign.Automation{WorkflowID: "wf-1"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if prepared.Metadata == nil || !reflect.DeepEqual(prepared.Metadata(map[string]any{"turma": " "}), []string{"turma"}) || len(prepared.Metadata(map[string]any{"turma": "A"})) != 0 {
			t.Fatalf("%s: the metadata rule is not the workflow's", name)
		}
		if _, err := prepare(Params{Automation: campaign.Automation{WorkflowID: "wf-x"}}); !errors.Is(err, campaign.ErrWorkflowNotFound) {
			t.Fatalf("%s: an unknown workflow = %v", name, err)
		}
	}
}

func TestWhatsAppRowsKeepTheirLineForTheReport(t *testing.T) {
	w := newWhatsAppWorld(t)
	prepared, err := w.official.Prepare(ctx, testActor, Params{TemplateID: "tpl-1"})
	if err != nil {
		t.Fatal(err)
	}
	judge, err := prepared.Open(ctx, boundOf(t, prepared), campaignaudience.SourceList)
	if err != nil {
		t.Fatal(err)
	}
	got, err := judge.Rows(ctx, []RowCandidate{{Row: campaignaudience.Row{ListRow: campaign.ListRow{Line: 7, Contact: "5511900000009"}}, Values: []string{"a", "b"}}})
	if err != nil || len(got) != 1 || got[0].Line != 7 {
		t.Fatalf("rows = %+v, %v", got, err)
	}
}

func TestOfficialChannelCountsLeadsBeforeATemplateIsChosen(t *testing.T) {
	w := newWhatsAppWorld(t)
	w.screener.skipped["busy"] = campaign.SkipBlocked
	prepared, err := w.official.Prepare(ctx, testActor, Params{})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Slots) != 0 || prepared.Awaits != campaignaudience.BlockerTemplateRequired {
		t.Fatalf("prepared = %+v", prepared)
	}
	judge, err := prepared.Open(ctx, nil, campaignaudience.SourceLeads)
	if err != nil {
		t.Fatal(err)
	}
	got, err := judge.Leads(ctx, []string{"ana", "busy"})
	if err != nil || len(got) != 2 || got[0].Refusal != "" || got[1].Refusal != string(campaign.SkipBlocked) {
		t.Fatalf("candidates = %+v, %v", got, err)
	}
	if !reflect.DeepEqual(w.screener.senders, []string{""}) {
		t.Fatalf("without a phone the cooldown sender is empty: %v", w.screener.senders)
	}
	unofficial, err := w.unoff.Prepare(ctx, testActor, Params{})
	if err != nil || len(unofficial.Slots) != 0 || unofficial.Awaits != campaignaudience.BlockerMessageRequired {
		t.Fatalf("unofficial = %+v, %v", unofficial, err)
	}
}

func TestOfficialLeadsTakeNamedAndHeaderVariablesInTheSendOrder(t *testing.T) {
	w := newWhatsAppWorld(t)
	prepared, err := w.official.Prepare(ctx, testActor, Params{TemplateID: "named"})
	if err != nil {
		t.Fatal(err)
	}
	want := []campaignaudience.Slot{{Key: "1", Name: "loja", Header: true}, {Key: "2", Name: "nome"}}
	if !reflect.DeepEqual(prepared.Slots, want) || prepared.Awaits != "" {
		t.Fatalf("slots = %+v", prepared.Slots)
	}
	judge, err := prepared.Open(ctx, boundOf(t, prepared, campaignaudience.Variable{Key: "1", Source: campaign.BindLiteral, Value: "Prisma"}, campaignaudience.Variable{Key: "2", Source: campaign.BindFirstName}), campaignaudience.SourceLeads)
	if err != nil {
		t.Fatal(err)
	}
	got, err := judge.Leads(ctx, []string{"ana"})
	if err != nil || !reflect.DeepEqual(got[0].Values, []string{"Prisma", "Ana"}) {
		t.Fatalf("values = %+v, %v", got, err)
	}
	body, header := (&template.Template{ParameterFormat: template.ParameterFormatNamed, Components: []template.TemplateComponent{{Type: "HEADER", Format: "TEXT", Text: "Loja {{loja}}"}, {Type: "BODY", Text: "Olá {{nome}}"}}}).SplitParameters(got[0].Values)
	if !reflect.DeepEqual(header, []string{"Prisma"}) || !reflect.DeepEqual(body, []string{"Ana"}) {
		t.Fatalf("the send splits header %q body %q", header, body)
	}
}
