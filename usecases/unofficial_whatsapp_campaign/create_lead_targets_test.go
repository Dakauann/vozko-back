package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type automationThatAllows struct{}

func (automationThatAllows) Check(string, campaign.Automation, []campaign.EntryMetadata) error {
	return nil
}

type automationThatRefuses struct{ err error }

func (a automationThatRefuses) Check(string, campaign.Automation, []campaign.EntryMetadata) error {
	return a.err
}

type workspaceLeads struct {
	byWorkspace map[string]map[string]*lead.Lead
	calls       [][]string
}

func (w *workspaceLeads) FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error) {
	w.calls = append(w.calls, append([]string(nil), ids...))
	out := []*lead.Lead{}
	for _, id := range ids {
		if l, ok := w.byWorkspace[workspaceID][id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

type keyedCampaigns struct {
	campaigns  *fakeCampaignRepo
	entries    *fakeEntryRepo
	entriesErr error
}

var errEntriesTimedOut = errors.New("the entry insert timed out")

func (k *keyedCampaigns) CreateWithEntries(c *uwc.Campaign, entries []uwc.Entry) error {
	if _, err := k.FindByIdempotencyKey(c.WorkspaceID, c.IdempotencyKey); err == nil {
		return campaign.ErrIdempotencyKeyTaken
	}
	if k.entriesErr != nil {
		err := k.entriesErr
		k.entriesErr = nil
		return err
	}
	if err := k.campaigns.Create(c); err != nil {
		return err
	}
	_, err := k.entries.CreateMany(entries)
	return err
}

func (k *keyedCampaigns) FindByIdempotencyKey(workspaceID, key string) (*uwc.Campaign, error) {
	k.campaigns.mu.Lock()
	defer k.campaigns.mu.Unlock()
	for _, c := range k.campaigns.campaigns {
		if c.WorkspaceID == workspaceID && c.IdempotencyKey == key {
			return c, nil
		}
	}
	return nil, uwc.ErrCampaignNotFound
}

type leadTargetHarness struct {
	uc        *createCampaignUseCase
	keys      *keyedCampaigns
	campaigns *fakeCampaignRepo
	entries   *fakeEntryRepo
	spam      *fakeSpam
	leads     *workspaceLeads
	numbers   *fakeLeadRepo
}

func newLeadTargetHarness() *leadTargetHarness {
	campaigns := newFakeCampaignRepo()
	entries := newFakeEntryRepo()
	gateway := &fakeGateway{instance: &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected}}
	spam := &fakeSpam{skip: map[string]bool{}, reasons: map[string]campaign.SkipReason{}}
	numbers := newFakeLeadRepo()
	h := &leadTargetHarness{campaigns: campaigns, entries: entries, spam: spam, numbers: numbers, leads: &workspaceLeads{byWorkspace: map[string]map[string]*lead.Lead{
		"ws-1": {
			"lead-a": {ID: "lead-a", WorkspaceID: "ws-1", Number: "5584999990001", Name: "Maria"},
			"lead-b": {ID: "lead-b", WorkspaceID: "ws-1", Number: "5584999990002"},
		},
		"ws-2": {"lead-x": {ID: "lead-x", WorkspaceID: "ws-2", Number: "5584999990009"}},
	}}}
	h.uc = NewCreateCampaignUseCase(campaigns, entries, numbers, gateway, spam, fakeDepartments{id: "dept-1"}).(*createCampaignUseCase)
	h.uc.SetAutomation(automationThatAllows{})
	h.uc.SetLeadsByID(h.leads)
	h.keys = &keyedCampaigns{campaigns: campaigns, entries: entries}
	h.uc.SetIdempotentCampaigns(h.keys)
	return h
}

func leadDraft(targets ...uwc.TargetInput) *uwc.Campaign {
	return &uwc.Campaign{
		WorkspaceID: "ws-1", InstanceID: "inst-1", Name: "Matrículas", Source: campaign.SourceLeadSelection,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Olá {{1}}"}}, Targets: targets,
	}
}

func (h *leadTargetHarness) entriesByLead() map[string]*uwc.Entry {
	h.entries.mu.Lock()
	defer h.entries.mu.Unlock()
	out := map[string]*uwc.Entry{}
	for _, e := range h.entries.entries {
		out[e.LeadID] = e
	}
	return out
}

func TestCreateResolvesLeadTargetsInsideTheWorkspaceAndSendsToTheirNumber(t *testing.T) {
	h := newLeadTargetHarness()
	_, err := h.uc.Execute(context.Background(), leadDraft(
		uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}},
		uwc.TargetInput{LeadID: "lead-x", Variables: []string{"X"}},
	), uw.Unrestricted())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(h.leads.calls) != 1 {
		t.Fatalf("FindByIDs calls = %v, want one", h.leads.calls)
	}
	created := h.entriesByLead()
	if len(created) != 1 || created["lead-a"] == nil || created["lead-a"].Number != "5584999990001" {
		t.Fatalf("entries = %+v, want only lead-a with its own number", created)
	}
	if len(h.numbers.byNum) != 0 {
		t.Fatal("a lead target created a lead")
	}
}

func TestCreateMarksTheSkipDecidedBeforeCreation(t *testing.T) {
	h := newLeadTargetHarness()
	h.spam.reasons["lead-b"] = campaign.SkipOptedOut
	_, err := h.uc.Execute(context.Background(), leadDraft(
		uwc.TargetInput{LeadID: "lead-a", Skip: campaign.SkipMissingVariable},
		uwc.TargetInput{LeadID: "lead-b", Skip: campaign.SkipMissingVariable},
	), uw.Unrestricted())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	created := h.entriesByLead()
	if e := created["lead-a"]; e == nil || e.ErrorCode != 920006 || e.Status != campaign.SendStatusFailed {
		t.Fatalf("lead-a = %+v, want missing_variable", e)
	}
	if e := created["lead-b"]; e == nil || e.ErrorCode != 920002 {
		t.Fatalf("lead-b = %+v, want opted_out to win", e)
	}
}

func TestCreateRecordsWhichVariableWasMissing(t *testing.T) {
	h := newLeadTargetHarness()
	h.spam.reasons["lead-b"] = campaign.SkipBlocked
	name := campaign.MissingVariable{Slot: 1, Source: campaign.BindName}
	district := campaign.MissingVariable{Slot: 2, Source: campaign.BindDistrict}
	_, err := h.uc.Execute(context.Background(), leadDraft(
		uwc.TargetInput{LeadID: "lead-a", Skip: campaign.SkipMissingVariable, Missing: []campaign.MissingVariable{name, district}},
		uwc.TargetInput{LeadID: "lead-b", Skip: campaign.SkipMissingVariable, Missing: []campaign.MissingVariable{district}},
	), uw.Unrestricted())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	created := h.entriesByLead()
	if e := created["lead-a"]; e == nil || e.ErrorCode != 920006 || e.ErrorMessage != "missing_variable:1:lead.name;2:lead.district" {
		t.Fatalf("lead-a = %+v, want both slots named", e)
	}
	if e := created["lead-b"]; e == nil || e.ErrorCode != 920001 || e.ErrorMessage != "blocked" {
		t.Fatalf("lead-b = %+v, want blocked to win without a slot", e)
	}
}

func TestCreateRecordsTheCooldownDaysTheGuardJudgedBy(t *testing.T) {
	h := newLeadTargetHarness()
	h.spam.skip["lead-a"] = true
	h.spam.days = 7
	_, err := h.uc.Execute(context.Background(), leadDraft(
		uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}},
		uwc.TargetInput{LeadID: "lead-b", Variables: []string{"Ana"}},
	), uw.Unrestricted())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	created := h.entriesByLead()
	if e := created["lead-a"]; e == nil || e.Status != campaign.SendStatusNotEligiblePossibleSpam || e.ErrorCode != 920004 || e.ErrorMessage != "cooldown:7" {
		t.Fatalf("lead-a = %+v, want the 7 days recorded", e)
	}
	if e := created["lead-b"]; e == nil || e.Status != campaign.SendStatusPending || e.ErrorMessage != "" {
		t.Fatalf("lead-b = %+v, want pending", e)
	}
}

func TestARetriedUnofficialCreationReturnsTheSameCampaign(t *testing.T) {
	h := newLeadTargetHarness()
	in := leadDraft(uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}})
	in.IdempotencyKey = "base:1/1"
	first, err := h.uc.Execute(context.Background(), in, uw.Unrestricted())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	retry := leadDraft(uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}})
	retry.IdempotencyKey = "base:1/1"
	again, err := h.uc.Execute(context.Background(), retry, uw.Unrestricted())
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if again.ID != first.ID || h.campaigns.count() != 1 || h.entries.count() != 1 {
		t.Fatalf("retry made %s (first %s), campaigns %d entries %d", again.ID, first.ID, h.campaigns.count(), h.entries.count())
	}
}

func TestAKeyedUnofficialCreationWhoseEntriesFailLeavesNothingForTheRetryToReturn(t *testing.T) {
	h := newLeadTargetHarness()
	h.keys.entriesErr = errEntriesTimedOut
	in := leadDraft(uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}}, uwc.TargetInput{LeadID: "lead-b", Variables: []string{"Ana"}})
	in.IdempotencyKey = "base:1/1"
	if _, err := h.uc.Execute(context.Background(), in, uw.Unrestricted()); !errors.Is(err, errEntriesTimedOut) {
		t.Fatalf("first = %v, want the entry failure", err)
	}
	if h.campaigns.count() != 0 {
		t.Fatal("a keyed campaign stayed without its entries")
	}
	retry := leadDraft(uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}}, uwc.TargetInput{LeadID: "lead-b", Variables: []string{"Ana"}})
	retry.IdempotencyKey = "base:1/1"
	if _, err := h.uc.Execute(context.Background(), retry, uw.Unrestricted()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if h.campaigns.count() != 1 || h.entries.count() != 2 {
		t.Fatalf("campaigns %d entries %d, want one campaign holding both leads", h.campaigns.count(), h.entries.count())
	}
}

func TestUnofficialLeadTargetsRefuseWithoutTheLeadLookup(t *testing.T) {
	h := newLeadTargetHarness()
	h.uc.SetLeadsByID(nil)
	if _, err := h.uc.Execute(context.Background(), leadDraft(uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}}), uw.Unrestricted()); !errors.Is(err, campaign.ErrLeadTargetsUnavailable) {
		t.Fatalf("Execute = %v, want ErrLeadTargetsUnavailable", err)
	}
	if h.campaigns.count() != 0 {
		t.Fatal("a campaign was written")
	}
}

func TestUnofficialCreateRunsTheSharedAutomationStep(t *testing.T) {
	h := newLeadTargetHarness()
	h.uc.SetAutomation(automationThatRefuses{err: campaign.ErrWorkflowVarsMissing})
	in := leadDraft(uwc.TargetInput{LeadID: "lead-a", Variables: []string{"Maria"}})
	in.WorkflowID, in.EnableWorkflow = "wf-1", true
	if _, err := h.uc.Execute(context.Background(), in, uw.Unrestricted()); !errors.Is(err, campaign.ErrWorkflowVarsMissing) {
		t.Fatalf("Execute = %v, want ErrWorkflowVarsMissing", err)
	}
	h.uc.SetAutomation(nil)
	if _, err := h.uc.Execute(context.Background(), in, uw.Unrestricted()); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Execute without the step = %v, want ErrAutomationUnavailable", err)
	}
	if h.campaigns.count() != 0 {
		t.Fatal("a refused campaign was written")
	}
}
