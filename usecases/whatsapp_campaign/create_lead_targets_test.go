package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/campaign"
	"vozko/domain/lead"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
	"vozko/usecases/campaignautomation"
)

type workspaceLeads struct {
	*numberedLeads
	byWorkspace map[string]map[string]*lead.Lead
	idCalls     [][]string
}

func (r *workspaceLeads) FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error) {
	r.idCalls = append(r.idCalls, append([]string(nil), ids...))
	out := []*lead.Lead{}
	for _, id := range ids {
		if l, ok := r.byWorkspace[workspaceID][id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

type keyedCampaigns struct {
	mu         sync.Mutex
	byKey      map[string]*wc.Campaign
	lookups    int
	lookupErr  error
	campaigns  *mockCampaignRepo
	entries    *capturingEntries
	entriesErr error
	raced      func()
}

var errEntriesTimedOut = errors.New("the entry insert timed out")

func (k *keyedCampaigns) CreateWithEntries(c *wc.Campaign, entries []wce.WhatsAppCampaignEntry) error {
	if k.raced != nil {
		k.raced()
		return campaign.ErrIdempotencyKeyTaken
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, taken := k.byKey[c.WorkspaceID+"|"+c.IdempotencyKey]; taken {
		return campaign.ErrIdempotencyKeyTaken
	}
	if k.entriesErr != nil {
		err := k.entriesErr
		k.entriesErr = nil
		return err
	}
	k.byKey[c.WorkspaceID+"|"+c.IdempotencyKey] = c
	if err := k.campaigns.Create(c); err != nil {
		return err
	}
	_, err := k.entries.CreateMany(entries)
	return err
}

func (k *keyedCampaigns) FindByIdempotencyKey(workspaceID, key string) (*wc.Campaign, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.lookups++
	if k.lookupErr != nil {
		return nil, k.lookupErr
	}
	if c, ok := k.byKey[workspaceID+"|"+key]; ok {
		return c, nil
	}
	return nil, wc.ErrCampaignNotFound
}

var errKeyedOutsideTheTransaction = errors.New("a keyed campaign was written without its entries")

type keyedCampaignRepo struct {
	*mockCampaignRepo
}

func (r *keyedCampaignRepo) Create(c *wc.Campaign) error {
	if c.IdempotencyKey != "" {
		return errKeyedOutsideTheTransaction
	}
	return r.mockCampaignRepo.Create(c)
}

type noWorkflows struct{ byID map[string]*workflow.Workflow }

func (n noWorkflows) Execute(id string) (*workflow.Workflow, error) {
	if wf, ok := n.byID[id]; ok {
		return wf, nil
	}
	return nil, workflow.ErrWorkflowNotFound
}

type noAgents struct{}

func (noAgents) Execute(string) (*agent.Agent, error) { return nil, agent.ErrAgentNotFound }

func permissiveAutomation(t *testing.T) *campaignautomation.Step {
	t.Helper()
	step, err := campaignautomation.NewStep(noWorkflows{}, noAgents{})
	if err != nil {
		t.Fatalf("NewStep: %v", err)
	}
	return step
}

type leadTargetFixture struct {
	*createFixture
	leads     *workspaceLeads
	keys      *keyedCampaigns
	campaigns *keyedCampaignRepo
}

func newLeadTargetFixture() *leadTargetFixture {
	base := newCreateFixture()
	keys := &keyedCampaigns{byKey: map[string]*wc.Campaign{}, campaigns: base.campaigns, entries: base.entries}
	return &leadTargetFixture{
		createFixture: base,
		leads: &workspaceLeads{numberedLeads: base.leads, byWorkspace: map[string]map[string]*lead.Lead{
			"ws-1": {
				"lead-a": {ID: "lead-a", WorkspaceID: "ws-1", Number: "5584999990001"},
				"lead-b": {ID: "lead-b", WorkspaceID: "ws-1", Number: "5584999990002"},
				"lead-c": {ID: "lead-c", WorkspaceID: "ws-1"},
			},
			"ws-2": {"lead-x": {ID: "lead-x", WorkspaceID: "ws-2", Number: "5584999990009"}},
		}},
		keys:      keys,
		campaigns: &keyedCampaignRepo{mockCampaignRepo: base.campaigns},
	}
}

func (f *leadTargetFixture) build(t *testing.T, automation AutomationCheck) wc.CreateCampaignUseCase {
	t.Helper()
	phones, templates := createPhonesAndTemplates()
	uc := NewCreateCampaignUseCase(f.campaigns, f.entries, f.leads, templates, phones, &ownershipWorkspacePhoneAccessRepo{hasAccess: true}, f.screener)
	concrete := uc.(*createCampaignUseCase)
	concrete.SetTemplateGrants(allowGrants{})
	concrete.SetAutomation(automation)
	concrete.SetIdempotentCampaigns(f.keys)
	return uc
}

func leadCampaign(inputs ...wc.PhoneInput) *wc.Campaign {
	return &wc.Campaign{
		WorkspaceID: "ws-1", Name: "Matrículas", TemplateID: "tmpl-1", BusinessPhoneID: "bp-1", Status: wc.CampaignStatusStopped,
		Source: campaign.SourceLeadSelection, PhoneInputs: inputs,
	}
}

func entriesByLead(entries []wce.WhatsAppCampaignEntry) map[string]wce.WhatsAppCampaignEntry {
	out := map[string]wce.WhatsAppCampaignEntry{}
	for _, e := range entries {
		out[e.LeadID] = e
	}
	return out
}

func TestCreateResolvesLeadTargetsInsideTheWorkspaceWithoutCreatingLeads(t *testing.T) {
	f := newLeadTargetFixture()
	_, err := f.build(t, permissiveAutomation(t)).Execute(context.Background(), leadCampaign(
		wc.PhoneInput{LeadID: "lead-a"}, wc.PhoneInput{LeadID: "lead-b"}, wc.PhoneInput{LeadID: "lead-x"},
	))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if f.leads.resolved != 0 {
		t.Fatal("lead targets must never go through FindOrCreateMany")
	}
	if len(f.leads.idCalls) != 1 || len(f.leads.idCalls[0]) != 3 {
		t.Fatalf("FindByIDs calls = %v, want one call with the three ids", f.leads.idCalls)
	}
	created := entriesByLead(f.entries.created)
	if len(created) != 2 {
		t.Fatalf("created entries for %v, want lead-a and lead-b only", created)
	}
	if _, foreign := created["lead-x"]; foreign {
		t.Fatal("a lead of another workspace got an entry")
	}
}

func TestCreateMixesNumberAndLeadTargets(t *testing.T) {
	f := newLeadTargetFixture()
	_, err := f.build(t, permissiveAutomation(t)).Execute(context.Background(), leadCampaign(
		wc.PhoneInput{LeadID: "lead-a"}, wc.PhoneInput{Number: "5584999990005"},
	))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	created := entriesByLead(f.entries.created)
	if _, ok := created["lead-a"]; !ok {
		t.Fatal("the lead target got no entry")
	}
	if _, ok := created["lead-5584999990005"]; !ok {
		t.Fatal("the number target got no entry")
	}
}

func TestCreateMarksTheSkipDecidedBeforeCreationAfterTheLeadRules(t *testing.T) {
	f := newLeadTargetFixture()
	f.screener.skip["lead-b"] = campaign.SkipBlocked
	_, err := f.build(t, permissiveAutomation(t)).Execute(context.Background(), leadCampaign(
		wc.PhoneInput{LeadID: "lead-a", Skip: campaign.SkipMissingVariable},
		wc.PhoneInput{LeadID: "lead-b", Skip: campaign.SkipAlreadyInRunningCampaign},
		wc.PhoneInput{LeadID: "lead-c"},
	))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	created := entriesByLead(f.entries.created)
	if e := created["lead-a"]; e.Status != wce.SendStatusFailed || e.ErrorCode != 920006 || e.ErrorMessage != "missing_variable" {
		t.Fatalf("lead-a entry = %+v, want missing_variable", e)
	}
	if e := created["lead-b"]; e.ErrorCode != 920001 {
		t.Fatalf("lead-b entry = %+v, want blocked to win", e)
	}
	if e := created["lead-c"]; e.Status != wce.SendStatusPending {
		t.Fatalf("lead-c entry = %+v, want pending", e)
	}
}

func TestCreateRecordsWhichVariableWasMissing(t *testing.T) {
	f := newLeadTargetFixture()
	f.screener.skip["lead-b"] = campaign.SkipOptedOut
	school := campaign.MissingVariable{Slot: 1, Source: campaign.CustomBinding("escola")}
	district := campaign.MissingVariable{Slot: 2, Source: campaign.BindDistrict}
	_, err := f.build(t, permissiveAutomation(t)).Execute(context.Background(), leadCampaign(
		wc.PhoneInput{LeadID: "lead-a", Skip: campaign.SkipMissingVariable, Missing: []campaign.MissingVariable{school, district}},
		wc.PhoneInput{LeadID: "lead-b", Skip: campaign.SkipMissingVariable, Missing: []campaign.MissingVariable{school}},
	))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	created := entriesByLead(f.entries.created)
	if e := created["lead-a"]; e.ErrorCode != 920006 || e.ErrorMessage != "missing_variable:1:lead.custom:escola;2:lead.district" {
		t.Fatalf("lead-a entry = %+v, want both slots named", e)
	}
	if e := created["lead-b"]; e.ErrorCode != 920002 || e.ErrorMessage != "opted_out" {
		t.Fatalf("lead-b entry = %+v, want opted_out to win without a slot", e)
	}
}

func TestCreateRecordsTheCooldownDaysTheGuardJudgedBy(t *testing.T) {
	f := newLeadTargetFixture()
	f.screener.skip["lead-a"] = campaign.SkipCooldown
	f.screener.days = 30
	_, err := f.build(t, permissiveAutomation(t)).Execute(context.Background(), leadCampaign(
		wc.PhoneInput{LeadID: "lead-a"},
		wc.PhoneInput{LeadID: "lead-b"},
	))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	created := entriesByLead(f.entries.created)
	if e := created["lead-a"]; e.Status != wce.SendStatusNotEligiblePossibleSpam || e.ErrorCode != 920004 || e.ErrorMessage != "cooldown:30" {
		t.Fatalf("lead-a entry = %+v, want the 30 days recorded", e)
	}
	if e := created["lead-b"]; e.Status != wce.SendStatusPending || e.ErrorMessage != "" {
		t.Fatalf("lead-b entry = %+v, want pending", e)
	}
}

func TestARetriedCreationReturnsTheSameCampaign(t *testing.T) {
	f := newLeadTargetFixture()
	uc := f.build(t, permissiveAutomation(t))
	first := leadCampaign(wc.PhoneInput{LeadID: "lead-a"})
	first.IdempotencyKey = "send-key:1/1"
	created, err := uc.Execute(context.Background(), first)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	retry := leadCampaign(wc.PhoneInput{LeadID: "lead-a"})
	retry.IdempotencyKey = "send-key:1/1"
	again, err := uc.Execute(context.Background(), retry)
	if err != nil {
		t.Fatalf("retried Execute: %v", err)
	}
	if again.ID != created.ID {
		t.Fatalf("retry returned %s, want %s", again.ID, created.ID)
	}
	if len(f.campaigns.campaigns) != 1 || len(f.entries.created) != 1 {
		t.Fatalf("campaigns %d entries %d, want one of each", len(f.campaigns.campaigns), len(f.entries.created))
	}
}

func TestACreationThatLosesTheKeyRaceReturnsTheWinner(t *testing.T) {
	f := newLeadTargetFixture()
	winner := &wc.Campaign{ID: "winner", WorkspaceID: "ws-1", Name: "Matrículas"}
	f.campaigns.mockCampaignRepo.campaigns["winner"] = winner
	f.keys.raced = func() {
		f.keys.byKey["ws-1|send-key:1/1"] = winner
	}
	in := leadCampaign(wc.PhoneInput{LeadID: "lead-a"})
	in.IdempotencyKey = "send-key:1/1"
	got, err := f.build(t, permissiveAutomation(t)).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ID != "winner" {
		t.Fatalf("Execute returned %s, want the winner", got.ID)
	}
	if len(f.entries.created) != 0 {
		t.Fatal("the loser wrote entries")
	}
}

func TestAKeyedCreationWhoseEntriesFailLeavesNothingForTheRetryToReturn(t *testing.T) {
	f := newLeadTargetFixture()
	f.keys.entriesErr = errEntriesTimedOut
	uc := f.build(t, permissiveAutomation(t))
	first := leadCampaign(wc.PhoneInput{LeadID: "lead-a"}, wc.PhoneInput{LeadID: "lead-b"})
	first.IdempotencyKey = "send-key:1/1"
	if _, err := uc.Execute(context.Background(), first); !errors.Is(err, errEntriesTimedOut) {
		t.Fatalf("first Execute = %v, want the entry failure", err)
	}
	if len(f.campaigns.campaigns) != 0 {
		t.Fatal("a keyed campaign stayed without its entries")
	}
	retry := leadCampaign(wc.PhoneInput{LeadID: "lead-a"}, wc.PhoneInput{LeadID: "lead-b"})
	retry.IdempotencyKey = "send-key:1/1"
	created, err := uc.Execute(context.Background(), retry)
	if err != nil {
		t.Fatalf("retried Execute: %v", err)
	}
	if created == nil || len(f.campaigns.campaigns) != 1 || len(f.entries.created) != 2 {
		t.Fatalf("campaigns %d entries %d, want one campaign holding both leads", len(f.campaigns.campaigns), len(f.entries.created))
	}
}

func TestAKeyedCreationRefusesWithoutTheKeyStore(t *testing.T) {
	f := newLeadTargetFixture()
	uc := f.build(t, permissiveAutomation(t))
	uc.(*createCampaignUseCase).SetIdempotentCampaigns(nil)
	in := leadCampaign(wc.PhoneInput{LeadID: "lead-a"})
	in.IdempotencyKey = "send-key:1/1"
	if _, err := uc.Execute(context.Background(), in); !errors.Is(err, campaign.ErrIdempotencyUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdempotencyUnavailable", err)
	}
	if len(f.campaigns.campaigns) != 0 {
		t.Fatal("a campaign was written without the key store")
	}
}

func TestCreateRefusesAMissingWorkflowVariableWithoutTheHandler(t *testing.T) {
	f := newLeadTargetFixture()
	workflows := noWorkflows{byID: map[string]*workflow.Workflow{"wf-1": {ID: "wf-1", WorkspaceID: "ws-1", Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "n1", Config: map[string]interface{}{"text": "{{campvars.escola}}"}},
	}}}}}
	step, err := campaignautomation.NewStep(workflows, noAgents{})
	if err != nil {
		t.Fatalf("NewStep: %v", err)
	}
	in := leadCampaign(wc.PhoneInput{LeadID: "lead-a"})
	in.WorkflowID, in.EnableWorkflow = "wf-1", true
	if _, err := f.build(t, step).Execute(context.Background(), in); !errors.Is(err, campaign.ErrWorkflowVarsMissing) {
		t.Fatalf("Execute = %v, want ErrWorkflowVarsMissing", err)
	}
	if len(f.campaigns.campaigns) != 0 || len(f.entries.created) != 0 {
		t.Fatal("a refused campaign wrote something")
	}
}

func TestCreateRefusesAnAgentItCannotRead(t *testing.T) {
	f := newLeadTargetFixture()
	in := leadCampaign(wc.PhoneInput{LeadID: "lead-a"})
	in.AgentID, in.EnableAgentResponses = "agent-1", true
	if _, err := f.build(t, permissiveAutomation(t)).Execute(context.Background(), in); !errors.Is(err, campaign.ErrAgentNotFound) {
		t.Fatalf("Execute = %v, want ErrAgentNotFound", err)
	}
}

func TestCreateRefusesWithoutTheAutomationStep(t *testing.T) {
	f := newLeadTargetFixture()
	if _, err := f.build(t, nil).Execute(context.Background(), leadCampaign(wc.PhoneInput{LeadID: "lead-a"})); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Execute = %v, want ErrAutomationUnavailable", err)
	}
}
