package stage_usecase

import (
	"testing"

	"vozko/domain/stage"
)

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
	if got[0].PipelineID != "p1" || got[1].PipelineID != "p2" {
		t.Fatalf("funnel order = %s, %s", got[0].PipelineID, got[1].PipelineID)
	}
	if got[0].PipelineName != "FUNIL UNIFECAF" || !got[0].IsDefault {
		t.Errorf("first funnel = %+v", got[0])
	}

	if len(got[0].Stages) != 2 ||
		got[0].Stages[0].ID != "s2" || got[0].Stages[1].ID != "s1" {
		t.Errorf("stage order inside p1 = %+v", got[0].Stages)
	}
	if len(got[1].Stages) != 1 || got[1].Stages[0].ID != "s3" {
		t.Errorf("p2 stages = %+v", got[1].Stages)
	}
}

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

func TestListFunnelStagesLeavesOutStagesWithNoConversationFunnel(t *testing.T) {
	funnels := &fakeFunnelLister{funnels: []Funnel{{ID: "p1", Name: "Atendimento"}}}
	stages := &fakeStageRepo{stages: []*stage.Stage{
		st("s1", "p1", "novo lead", 1),
		st("orphan", "", "em atendimento", 1),
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
