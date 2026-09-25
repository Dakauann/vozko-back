package copilottools

import (
	"context"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/copilot"
	lm "vozko/domain/lead_memory"
)

type fakeMemoryWrites struct {
	created []lm.CreateInput
	updated []lm.UpdateInput
	err     error
}

type createAdapter struct{ f *fakeMemoryWrites }

func (c createAdapter) Execute(_ context.Context, in lm.CreateInput) (*lm.CreateResult, error) {
	c.f.created = append(c.f.created, in)
	if c.f.err != nil {
		return nil, c.f.err
	}
	return &lm.CreateResult{Memory: &lm.LeadMemory{ID: "m1", Content: in.Content, Category: in.Category}}, nil
}

type updateAdapter struct{ f *fakeMemoryWrites }

func (u updateAdapter) Execute(_ context.Context, in lm.UpdateInput) (*lm.LeadMemory, error) {
	u.f.updated = append(u.f.updated, in)
	if u.f.err != nil {
		return nil, u.f.err
	}
	return &lm.LeadMemory{ID: in.MemoryRef, Content: in.Content, Category: in.Category}, nil
}

func memoryDeps(leads *fakeLeads, writes *fakeMemoryWrites) LeadMemoryDeps {
	return LeadMemoryDeps{Leads: leads, Create: createAdapter{writes}, Update: updateAdapter{writes}}
}

func TestLeadMemoryChangesNeedApprovalAndLeadUpdate(t *testing.T) {
	deps := memoryDeps(&fakeLeads{}, &fakeMemoryWrites{})
	for _, tool := range []copilot.Tool{NewAddLeadMemoryTool(deps), NewUpdateLeadMemoryTool(deps)} {
		if m := tool.Meta(); !m.Mutating || m.Resource != "leads" || m.Action != "update" {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestAddLeadMemoryPublishesTheDomainCategories(t *testing.T) {
	def := NewAddLeadMemoryTool(memoryDeps(&fakeLeads{}, &fakeMemoryWrites{})).Definition()
	if got := def.Parameters["category"].Enum; len(got) != len(lm.AllCategories()) {
		t.Fatalf("category enum = %v", got)
	}
}

func TestAddLeadMemoryWritesAsTheUser(t *testing.T) {
	writes := &fakeMemoryWrites{}
	res := NewAddLeadMemoryTool(memoryDeps(&fakeLeads{}, writes)).Execute(context.Background(), member(), map[string]interface{}{
		"lead_id": knownLead, "content": "prefere boleto", "category": "preference",
	})
	if res.Status != copilot.StatusOK || len(writes.created) != 1 {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	in := writes.created[0]
	if in.WorkspaceID != "ws-1" || in.LeadID != knownLead || in.Actor != (lm.WriteActor{Kind: actor.KindHuman, ID: "u-1"}) || in.Category != lm.CategoryPreference {
		t.Fatalf("input = %+v", in)
	}
}

func TestAddLeadMemoryRefusesAnUnknownLeadOrCategory(t *testing.T) {
	writes := &fakeMemoryWrites{}
	for _, args := range []map[string]interface{}{
		{"lead_id": "maria", "content": "x", "category": "preference"},
		{"lead_id": knownLead, "content": "x", "category": "gossip"},
	} {
		if res := NewAddLeadMemoryTool(memoryDeps(&fakeLeads{}, writes)).Execute(context.Background(), member(), args); res.Status != copilot.StatusError {
			t.Fatalf("args %v: status %s", args, res.Status)
		}
	}
	if len(writes.created) != 0 {
		t.Fatal("wrote with bad arguments")
	}
}

func TestUpdateLeadMemoryCorrectsOneNote(t *testing.T) {
	writes := &fakeMemoryWrites{}
	res := NewUpdateLeadMemoryTool(memoryDeps(&fakeLeads{}, writes)).Execute(context.Background(), member(), map[string]interface{}{
		"lead_id": knownLead, "memory_id": "m1", "content": "prefere pix", "category": "preference",
	})
	if res.Status != copilot.StatusOK || writes.updated[0].MemoryRef != "m1" || writes.updated[0].Actor.ID != "u-1" {
		t.Fatalf("status %s updated %+v", res.Status, writes.updated)
	}
}

func TestLeadMemoryExplainsDomainRefusals(t *testing.T) {
	for _, err := range []error{lm.ErrDuplicate, lm.ErrLimitReached, lm.ErrContentTooLong} {
		res := NewAddLeadMemoryTool(memoryDeps(&fakeLeads{}, &fakeMemoryWrites{err: err})).Execute(context.Background(), member(), map[string]interface{}{
			"lead_id": knownLead, "content": "x", "category": "other",
		})
		if res.Status != copilot.StatusError || res.Message == "falha ao salvar a memória" {
			t.Fatalf("%v: %+v", err, res)
		}
	}
}

func TestLeadMemoryDescribesTheContactByName(t *testing.T) {
	tool := NewAddLeadMemoryTool(memoryDeps(&fakeLeads{}, &fakeMemoryWrites{}))
	fields := tool.(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{
		"lead_id": knownLead, "content": "prefere boleto", "category": "preference",
	})
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["contact"] != "Maria (••••9624)" || got["content"] != "prefere boleto" || got["category"] != "preference" {
		t.Fatalf("fields = %v", fields)
	}
}
