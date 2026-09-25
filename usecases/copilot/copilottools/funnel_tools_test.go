package copilottools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/pipeline"
	"vozko/domain/stage"
)

const knownPipeline = "4e5f6a7b-8c9d-4e0f-a1b2-c3d4e5f6a7b8"

type fakeCreatePipeline struct {
	inputs []pipeline.CreatePipelineInput
}

func (f *fakeCreatePipeline) Execute(_ string, in pipeline.CreatePipelineInput) (*pipeline.Pipeline, error) {
	f.inputs = append(f.inputs, in)
	return &pipeline.Pipeline{ID: knownPipeline, Name: in.Name}, nil
}

type fakeStageWrites struct {
	created   []stage.CreateStageInput
	renamed   []string
	reordered []stage.ReorderStagesInput
	initial   []string
	err       error
}

func (f *fakeStageWrites) Create(_ string, in stage.CreateStageInput) (*stage.Stage, error) {
	f.created = append(f.created, in)
	return &stage.Stage{ID: knownStage, Name: in.Name}, f.err
}

func (f *fakeStageWrites) Update(_ string, id string, in stage.UpdateStageInput) (*stage.Stage, error) {
	f.renamed = append(f.renamed, id+"|"+*in.Name)
	return &stage.Stage{ID: id, Name: *in.Name}, f.err
}

func (f *fakeStageWrites) Reorder(_ string, in stage.ReorderStagesInput) ([]*stage.Stage, error) {
	f.reordered = append(f.reordered, in)
	return nil, f.err
}

func (f *fakeStageWrites) SetInitial(_ string, id string) (*stage.Stage, error) {
	f.initial = append(f.initial, id)
	return &stage.Stage{ID: id}, f.err
}

type stageCreateFunc struct{ f *fakeStageWrites }

func (s stageCreateFunc) Execute(ws string, in stage.CreateStageInput) (*stage.Stage, error) {
	return s.f.Create(ws, in)
}

type stageUpdateFunc struct{ f *fakeStageWrites }

func (s stageUpdateFunc) Execute(ws, id string, in stage.UpdateStageInput) (*stage.Stage, error) {
	return s.f.Update(ws, id, in)
}

type stageReorderFunc struct{ f *fakeStageWrites }

func (s stageReorderFunc) Execute(ws string, in stage.ReorderStagesInput) ([]*stage.Stage, error) {
	return s.f.Reorder(ws, in)
}

type stageInitialFunc struct{ f *fakeStageWrites }

func (s stageInitialFunc) Execute(ws, id string) (*stage.Stage, error) { return s.f.SetInitial(ws, id) }

type funnelFixture struct{}

func (funnelFixture) Execute(string) ([]stage.FunnelStages, error) {
	return []stage.FunnelStages{{PipelineID: knownPipeline, PipelineName: "Vendas", Stages: []*stage.Stage{
		{ID: knownStage, Name: "Proposta"}, {ID: knownEntry, Name: "Fechado"},
	}}}, nil
}

func funnelDeps(pipelines *fakeCreatePipeline, stages *fakeStageWrites) FunnelDeps {
	return FunnelDeps{
		CreatePipeline: pipelines,
		CreateStage:    stageCreateFunc{stages},
		UpdateStage:    stageUpdateFunc{stages},
		ReorderStages:  stageReorderFunc{stages},
		SetInitial:     stageInitialFunc{stages},
		Funnels:        funnelFixture{},
	}
}

func TestFunnelToolsNeedApprovalAndTheStagePermissions(t *testing.T) {
	deps := funnelDeps(&fakeCreatePipeline{}, &fakeStageWrites{})
	want := map[string]string{
		"create_pipeline": "stages:create", "create_stage": "stages:create",
		"rename_stage": "stages:update", "reorder_stages": "stages:update", "set_initial_stage": "stages:update",
	}
	for _, tool := range []copilot.Tool{NewCreatePipelineTool(deps), NewCreateStageTool(deps), NewRenameStageTool(deps), NewReorderStagesTool(deps), NewSetInitialStageTool(deps)} {
		m := tool.Meta()
		if got := string(m.Resource) + ":" + string(m.Action); !m.Mutating || got != want[tool.Definition().Name] {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestCreatePipelineCreatesAConversationFunnel(t *testing.T) {
	pipelines := &fakeCreatePipeline{}
	res := NewCreatePipelineTool(funnelDeps(pipelines, &fakeStageWrites{})).Execute(context.Background(), member(), map[string]interface{}{
		"name": "Pós-venda", "stages": []interface{}{"Novo", "Resolvido"},
	})
	if res.Status != copilot.StatusOK || pipelines.inputs[0].ObjectType != string(pipeline.ObjectConversation) || len(pipelines.inputs[0].Stages) != 2 {
		t.Fatalf("status %s inputs %+v", res.Status, pipelines.inputs)
	}
}

func TestCreateStageLandsOnTheNamedFunnel(t *testing.T) {
	stages := &fakeStageWrites{}
	res := NewCreateStageTool(funnelDeps(&fakeCreatePipeline{}, stages)).Execute(context.Background(), member(), map[string]interface{}{
		"pipeline_id": knownPipeline, "name": "Negociação", "description": "valores em conversa",
	})
	if res.Status != copilot.StatusOK || stages.created[0].PipelineID != knownPipeline {
		t.Fatalf("status %s created %+v", res.Status, stages.created)
	}
}

func TestReorderStagesDescribesTheNewOrderByName(t *testing.T) {
	tool := NewReorderStagesTool(funnelDeps(&fakeCreatePipeline{}, &fakeStageWrites{}))
	fields := tool.(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{
		"pipeline_id": knownPipeline, "stage_ids": []interface{}{knownEntry, knownStage},
	})
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["pipeline"] != "Vendas" || got["order"] != "Fechado, Proposta" {
		t.Fatalf("fields = %v", fields)
	}
}

func TestRenameAndInitialStageActOnTheGivenStage(t *testing.T) {
	stages := &fakeStageWrites{}
	deps := funnelDeps(&fakeCreatePipeline{}, stages)
	NewRenameStageTool(deps).Execute(context.Background(), member(), map[string]interface{}{"stage_id": knownStage, "name": "Proposta enviada"})
	NewSetInitialStageTool(deps).Execute(context.Background(), member(), map[string]interface{}{"stage_id": knownStage})
	if len(stages.renamed) != 1 || stages.renamed[0] != knownStage+"|Proposta enviada" || len(stages.initial) != 1 {
		t.Fatalf("renamed %v initial %v", stages.renamed, stages.initial)
	}
}

func TestFunnelToolsRefuseInventedIdsAndExplainRefusals(t *testing.T) {
	stages := &fakeStageWrites{}
	deps := funnelDeps(&fakeCreatePipeline{}, stages)
	NewSetInitialStageTool(deps).Execute(context.Background(), member(), map[string]interface{}{"stage_id": "novo"})
	if len(stages.initial) != 0 {
		t.Fatal("acted on an invented stage")
	}
	res := NewCreateStageTool(funnelDeps(&fakeCreatePipeline{}, &fakeStageWrites{err: stage.ErrUnauthorized})).Execute(context.Background(), member(), map[string]interface{}{
		"pipeline_id": knownPipeline, "name": "x", "description": "y",
	})
	if res.Status != copilot.StatusDenied {
		t.Fatalf("status %s", res.Status)
	}
}
