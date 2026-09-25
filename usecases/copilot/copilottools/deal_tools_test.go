package copilottools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/opportunity"
	"vozko/domain/pipeline"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

const knownDeal = "6c7d8e9f-0a1b-4c2d-8e3f-4a5b6c7d8e9f"

type fakeDeals struct {
	by     shared.Person
	drafts []opportunity.DealDraft
	moves  []string
	links  []string
	listed string
	err    error
}

func (f *fakeDeals) Scope(shared.Person, string) (opportunity.DealScope, error) {
	return opportunity.DealScope{}, f.err
}

func (f *fakeDeals) Get(_ shared.Person, _ string, id string) (*opportunity.Opportunity, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &opportunity.Opportunity{ID: id, Title: "Plano anual", StageID: knownStage, ValueCents: 120000, Currency: "BRL", Status: opportunity.StatusOpen}, nil
}

func (f *fakeDeals) Create(by shared.Person, _ string, d opportunity.DealDraft) (*opportunity.Opportunity, error) {
	f.by = by
	f.drafts = append(f.drafts, d)
	if f.err != nil {
		return nil, f.err
	}
	return &opportunity.Opportunity{ID: knownDeal, Title: d.Title}, nil
}

func (f *fakeDeals) Move(by shared.Person, _ string, dealID, stageID string) (*opportunity.Opportunity, error) {
	f.by = by
	f.moves = append(f.moves, dealID+"|"+stageID)
	return &opportunity.Opportunity{ID: dealID, StageID: stageID}, f.err
}

func (f *fakeDeals) Link(by shared.Person, _ string, dealID, entryID, entryType string) error {
	f.by = by
	f.links = append(f.links, dealID+"|"+entryID+"|"+entryType)
	return f.err
}

func (f *fakeDeals) ListByPipeline(by shared.Person, _ string, pipelineID string) ([]*opportunity.Opportunity, error) {
	f.by = by
	f.listed = pipelineID
	return []*opportunity.Opportunity{{ID: knownDeal, Title: "Plano anual", StageID: knownStage, ValueCents: 120000, Currency: "BRL", Status: opportunity.StatusOpen}}, f.err
}

type fakeDealPipelines struct{ objectType string }

func (f *fakeDealPipelines) Execute(_ string, objectType string) ([]*pipeline.Pipeline, error) {
	f.objectType = objectType
	return []*pipeline.Pipeline{{ID: knownPipeline, Name: "Vendas", ObjectType: pipeline.ObjectType(objectType)}}, nil
}

type fakeDealStages struct{}

func (fakeDealStages) Execute(_, _, _, pipelineID string) ([]*stage.Stage, error) {
	return []*stage.Stage{{ID: knownStage, Name: "Proposta", PipelineID: pipelineID}}, nil
}

func dealDeps(deals *fakeDeals, pipelines *fakeDealPipelines) DealDeps {
	return DealDeps{Deals: deals, Pipelines: pipelines, Stages: fakeDealStages{}, Entries: &fakeEntries{}}
}

func TestDealToolsCheckTheDealRoutesPermissions(t *testing.T) {
	deps := dealDeps(&fakeDeals{}, &fakeDealPipelines{})
	want := map[string]struct {
		perm     string
		mutating bool
	}{
		"list_deal_pipelines": {"stages:read", false},
		"list_deals":          {"conversations:read", false},
		"create_deal":         {"conversations:create", true},
		"move_deal":           {"conversations:update", true},
		"link_deal":           {"conversations:update", true},
	}
	for _, tool := range []copilot.Tool{NewListDealPipelinesTool(deps), NewListDealsTool(deps), NewCreateDealTool(deps), NewMoveDealTool(deps), NewLinkDealTool(deps)} {
		m, w := tool.Meta(), want[tool.Definition().Name]
		if string(m.Resource)+":"+string(m.Action) != w.perm || m.Mutating != w.mutating {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestListDealPipelinesListsOnlyDealFunnelsWithStages(t *testing.T) {
	pipelines := &fakeDealPipelines{}
	res := NewListDealPipelinesTool(dealDeps(&fakeDeals{}, pipelines)).Execute(context.Background(), member(), nil)
	b, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || pipelines.objectType != string(pipeline.ObjectOpportunity) || !strings.Contains(string(b), `"stage_id":"`+knownStage) {
		t.Fatalf("status %s type %q data %s", res.Status, pipelines.objectType, b)
	}
}

func TestListDealsListsAsTheUser(t *testing.T) {
	deals := &fakeDeals{}
	res := NewListDealsTool(dealDeps(deals, &fakeDealPipelines{})).Execute(context.Background(), member(), map[string]interface{}{"pipeline_id": knownPipeline})
	b, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || deals.by.UserID != "u-1" || deals.listed != knownPipeline || !strings.Contains(string(b), `"stage":"Proposta"`) {
		t.Fatalf("status %s data %s", res.Status, b)
	}
}

func TestCreateDealCreatesAsTheUserOnTheConversation(t *testing.T) {
	deals := &fakeDeals{}
	res := NewCreateDealTool(dealDeps(deals, &fakeDealPipelines{})).Execute(context.Background(), member(), map[string]interface{}{
		"pipeline_id": knownPipeline, "stage_id": knownStage, "title": "Plano anual", "value": 1200.5,
		"entry_id": knownEntry, "entry_type": "whatsapp",
	})
	if res.Status != copilot.StatusOK || deals.by.UserID != "u-1" {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	d := deals.drafts[0]
	if d.ValueCents != 120050 || d.EntryID != knownEntry || d.PipelineID != knownPipeline {
		t.Fatalf("draft = %+v", d)
	}
}

func TestMoveDealDescribesTheDealAndStageByName(t *testing.T) {
	fields := NewMoveDealTool(dealDeps(&fakeDeals{}, &fakeDealPipelines{})).(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{
		"deal_id": knownDeal, "pipeline_id": knownPipeline, "stage_id": knownStage,
	})
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["deal"] != "Plano anual" || got["stage"] != "Proposta" {
		t.Fatalf("fields = %v", fields)
	}
}

func TestLinkDealRefusesAConversationTheUserCannotSee(t *testing.T) {
	deals := &fakeDeals{err: opportunity.ErrEntryAccess}
	res := NewLinkDealTool(dealDeps(deals, &fakeDealPipelines{})).Execute(context.Background(), member(), map[string]interface{}{
		"deal_id": knownDeal, "entry_id": knownEntry, "entry_type": "whatsapp",
	})
	if res.Status != copilot.StatusDenied {
		t.Fatalf("status %s", res.Status)
	}
}

func TestDealToolsRefuseInventedIds(t *testing.T) {
	deals := &fakeDeals{}
	NewMoveDealTool(dealDeps(deals, &fakeDealPipelines{})).Execute(context.Background(), member(), map[string]interface{}{
		"deal_id": "plano", "pipeline_id": knownPipeline, "stage_id": knownStage,
	})
	if len(deals.moves) != 0 {
		t.Fatal("moved an invented deal")
	}
}
