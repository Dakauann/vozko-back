package stage_usecase

import (
	"testing"

	"vozko/domain/stage"
)

// The inbox stage filter used to offer ONE funnel's stages: whatever
// ListByCampaign resolved, which is the campaign's funnel or the workspace
// default. In a workspace with several funnels that means an agent filters by a
// stage no conversation in front of them carries, and gets nothing. UniFecaf hit
// exactly that, filtering by a stage of a funnel they had renamed "NÃO USAR".
//
// This read exists so the filter can offer EVERY funnel, grouped, and so the
// grouping comes from one query rather than the client joining two lists it
// fetched separately and might have fetched at different moments.

type fakeFunnelLister struct {
	funnels []Funnel
	err     error
	askedWS string
}

func (f *fakeFunnelLister) ListConversationFunnels(workspaceID string) ([]Funnel, error) {
	f.askedWS = workspaceID
	return f.funnels, f.err
}

type fakeStageRepo struct {
	stage.Repository
	stages  []*stage.Stage
	err     error
	askedWS string
}

func (r *fakeStageRepo) ListByWorkspace(workspaceID string) ([]*stage.Stage, error) {
	r.askedWS = workspaceID
	return r.stages, r.err
}

func st(id, pipelineID, name string, position int) *stage.Stage {
	return &stage.Stage{ID: id, WorkspaceID: "ws", PipelineID: pipelineID, Name: name, Position: position}
}

func TestListFunnelStagesGroupsStagesUnderTheirFunnel(t *testing.T) {
	funnels := &fakeFunnelLister{funnels: []Funnel{
		{ID: "p1", Name: "FUNIL UNIFECAF", IsDefault: true, Position: 0},
		{ID: "p2", Name: "NÃO USAR", Position: 1},
	}}
	stages := &fakeStageRepo{stages: []*stage.Stage{
		st("s3", "p2", "agendamento", 1),
		st("s1", "p1", "inscrição", 2),
		st("s2", "p1", "novo lead", 1),
	}}

	got, err := NewListFunnelStagesUseCase(stages, funnels).Execute("ws")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("funnels = %d, want 2", len(got))
	}
	// Funnel order follows the funnel list, which is already ordered by
	// position: the filter renders these as groups and the order must match
	// what the funnels page shows.
	if got[0].PipelineID != "p1" || got[1].PipelineID != "p2" {
		t.Fatalf("funnel order = %s, %s", got[0].PipelineID, got[1].PipelineID)
	}
	if got[0].PipelineName != "FUNIL UNIFECAF" || !got[0].IsDefault {
		t.Errorf("first funnel = %+v", got[0])
	}

	// Stages inside a funnel keep their own position order, so the filter reads
	// like the funnel does on the board rather than in insertion order.
	if len(got[0].Stages) != 2 ||
		got[0].Stages[0].ID != "s2" || got[0].Stages[1].ID != "s1" {
		t.Errorf("stage order inside p1 = %+v", got[0].Stages)
	}
	if len(got[1].Stages) != 1 || got[1].Stages[0].ID != "s3" {
		t.Errorf("p2 stages = %+v", got[1].Stages)
	}
}

// A funnel with no columns yet is still listed. Dropping it would make the
// funnel invisible in the filter and leave an operator wondering where it went,
// which is worse than an empty group.
func TestListFunnelStagesKeepsAFunnelWithNoStages(t *testing.T) {
	funnels := &fakeFunnelLister{funnels: []Funnel{
		{ID: "p1", Name: "cheio"},
		{ID: "empty", Name: "vazio"},
	}}
	stages := &fakeStageRepo{stages: []*stage.Stage{st("s1", "p1", "novo lead", 1)}}

	got, err := NewListFunnelStagesUseCase(stages, funnels).Execute("ws")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("funnels = %d, want both listed", len(got))
	}
	if got[1].PipelineID != "empty" || len(got[1].Stages) != 0 {
		t.Errorf("empty funnel = %+v", got[1])
	}
}

// A stage that belongs to no CONVERSATION funnel is left out entirely.
//
// These used to ride a trailing "Sem funil" group, on the reasoning that such a
// stage "still filters and is still assigned". Production refutes that, and the
// numbers are not close: 24.158 of 27.845 live stages carry no pipeline_id,
// they are per-campaign clones the pipeline migration left behind, and the
// number of conversations sitting on ANY of them is ZERO — against 316.518 on
// real funnel stages.
//
// Keeping them put 728 entries in Anhanguera's stage filter and 2.120 in CDT
// IMPERATRIZ's, mostly the same handful of names repeated, which is exactly the
// unusable dropdown this whole feature exists to fix. A guess about what might
// be filterable lost to a measurement of what is.
//
// The same rule drops a stage on an OPPORTUNITY funnel, which is correct for a
// different reason: a deal stage can never hold a conversation.
func TestListFunnelStagesLeavesOutStagesWithNoConversationFunnel(t *testing.T) {
	funnels := &fakeFunnelLister{funnels: []Funnel{{ID: "p1", Name: "Atendimento"}}}
	stages := &fakeStageRepo{stages: []*stage.Stage{
		st("s1", "p1", "novo lead", 1),
		// No funnel at all: the legacy per-campaign clone shape.
		st("orphan", "", "em atendimento", 1),
		// A funnel this workspace's conversation funnels do not include, which
		// is what a sales funnel's stage looks like from here.
		st("deal", "sales-funnel", "ganho", 2),
	}}

	got, err := NewListFunnelStagesUseCase(stages, funnels).Execute("ws")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("groups = %d, want only the conversation funnel: %+v", len(got), got)
	}
	if got[0].PipelineID != "p1" || len(got[0].Stages) != 1 {
		t.Fatalf("group = %+v, want p1 holding only s1", got[0])
	}
	if got[0].Stages[0].ID != "s1" {
		t.Errorf("kept the wrong stage: %+v", got[0].Stages[0])
	}
}

// A workspace whose stages ALL lack a funnel yields no groups at all, rather
// than one enormous unusable one. The client falls back to its flat list, which
// is a worse filter than the grouped one and a far better one than 2.000 rows
// of the same four names.
func TestListFunnelStagesReturnsNothingWhenNoStageHasAFunnel(t *testing.T) {
	funnels := &fakeFunnelLister{}
	stages := &fakeStageRepo{stages: []*stage.Stage{
		st("a", "", "em atendimento", 1),
		st("b", "", "em atendimento", 2),
		st("c", "", "recebido", 3),
	}}

	got, err := NewListFunnelStagesUseCase(stages, funnels).Execute("ws")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("groups = %d, want none: %+v", len(got), got)
	}
}

// A funnel with no columns is still listed, so an operator who just created one
// can see it. Unchanged.
func TestListFunnelStagesKeepsAnEmptyFunnelListed(t *testing.T) {
	funnels := &fakeFunnelLister{funnels: []Funnel{{ID: "p1", Name: "Atendimento"}}}
	stages := &fakeStageRepo{stages: []*stage.Stage{st("s1", "p1", "novo lead", 1)}}

	got, err := NewListFunnelStagesUseCase(stages, funnels).Execute("ws")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("groups = %d, want just the one funnel", len(got))
	}
}

func TestListFunnelStagesRequiresAWorkspace(t *testing.T) {
	got, err := NewListFunnelStagesUseCase(&fakeStageRepo{}, &fakeFunnelLister{}).Execute("  ")
	if err == nil {
		t.Fatal("an empty workspace must be refused, not answered with every tenant's funnels")
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}
