package copilottools

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/pipeline"
	"vozko/domain/stage"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type FunnelDeps struct {
	CreatePipeline pipeline.CreatePipelineUseCase
	CreateStage    stage.CreateStageUseCase
	UpdateStage    stage.UpdateStageUseCase
	ReorderStages  stage.ReorderStagesUseCase
	SetInitial     stage.SetInitialStageUseCase
	Funnels        stage.ListFunnelStagesUseCase
}

func stagesMeta(action workspace.Action) copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceStages, Action: action}
}

type funnelNames struct {
	pipelines map[string]string
	stages    map[string]string
}

func namesOf(deps FunnelDeps, cc copilot.Context) funnelNames {
	names := funnelNames{pipelines: map[string]string{}, stages: map[string]string{}}
	funnels, err := deps.Funnels.Execute(cc.WorkspaceID)
	if err != nil {
		return names
	}
	for _, f := range funnels {
		names.pipelines[f.PipelineID] = f.PipelineName
		for _, s := range f.Stages {
			if s != nil {
				names.stages[s.ID] = s.Name
			}
		}
	}
	return names
}

func (n funnelNames) pipeline(id string) string {
	return orUnknown(n.pipelines[id], "funil desconhecido")
}
func (n funnelNames) stage(id string) string { return orUnknown(n.stages[id], "etapa desconhecida") }

func orUnknown(name, unknown string) string {
	if name == "" {
		return unknown
	}
	return name
}

type createPipelineArgs struct {
	Name   string   `json:"name" req:"true" desc:"nome do funil"`
	Stages []string `json:"stages" desc:"nomes das etapas, na ordem; omita para as etapas padrão"`
}

type createPipelineTool struct{ deps FunnelDeps }

func NewCreatePipelineTool(deps FunnelDeps) copilot.Tool { return &createPipelineTool{deps: deps} }

func (t *createPipelineTool) Meta() copilot.Meta { return stagesMeta(workspace.ActionCreate) }

func (t *createPipelineTool) Definition() tools.Definition {
	return definition("create_pipeline", "Cria um funil de atendimento com as etapas informadas. Só depois da aprovação do usuário.", createPipelineArgs{})
}

func (t *createPipelineTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createPipelineArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	seeds := make([]pipeline.StageSeed, 0, len(a.Stages))
	for _, name := range a.Stages {
		if name = strings.TrimSpace(name); name != "" {
			seeds = append(seeds, pipeline.StageSeed{Name: name})
		}
	}
	created, err := t.deps.CreatePipeline.Execute(cc.WorkspaceID, pipeline.CreatePipelineInput{
		Name: strings.TrimSpace(a.Name), ObjectType: string(pipeline.ObjectConversation), Stages: seeds,
	})
	if err != nil {
		return funnelFailure("create_pipeline", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"pipeline_id": created.ID, "name": created.Name}}
}

type createStageArgs struct {
	PipelineID  string `json:"pipeline_id" req:"true" desc:"pipeline_id exato de list_pipelines" id:"true"`
	Name        string `json:"name" req:"true" desc:"nome da etapa"`
	Description string `json:"description" req:"true" desc:"o que significa estar nessa etapa"`
}

type createStageTool struct{ deps FunnelDeps }

func NewCreateStageTool(deps FunnelDeps) copilot.Tool { return &createStageTool{deps: deps} }

func (t *createStageTool) Meta() copilot.Meta { return stagesMeta(workspace.ActionCreate) }

func (t *createStageTool) Definition() tools.Definition {
	return definition("create_stage", "Cria uma etapa no fim de um funil. Só depois da aprovação do usuário.", createStageArgs{})
}

func (t *createStageTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createStageArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "pipeline", Value: namesOf(t.deps, cc).pipeline(strings.TrimSpace(a.PipelineID))},
		{Key: "name", Value: strings.TrimSpace(a.Name)},
		{Key: "description", Value: strings.TrimSpace(a.Description)},
	}
}

func (t *createStageTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createStageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	pipelineID, err := knownID(a.PipelineID, "pipeline_id", "list_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	created, err := t.deps.CreateStage.Execute(cc.WorkspaceID, stage.CreateStageInput{Name: a.Name, Description: a.Description, PipelineID: pipelineID})
	if err != nil {
		return funnelFailure("create_stage", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"stage_id": created.ID, "name": created.Name}}
}

type renameStageArgs struct {
	StageID string `json:"stage_id" req:"true" desc:"stage_id exato de list_pipelines" id:"true"`
	Name    string `json:"name" req:"true" desc:"novo nome"`
}

type renameStageTool struct{ deps FunnelDeps }

func NewRenameStageTool(deps FunnelDeps) copilot.Tool { return &renameStageTool{deps: deps} }

func (t *renameStageTool) Meta() copilot.Meta { return stagesMeta(workspace.ActionUpdate) }

func (t *renameStageTool) Definition() tools.Definition {
	return definition("rename_stage", "Renomeia uma etapa. Só depois da aprovação do usuário.", renameStageArgs{})
}

func (t *renameStageTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a renameStageArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "stage", Value: namesOf(t.deps, cc).stage(strings.TrimSpace(a.StageID))},
		{Key: "name", Value: strings.TrimSpace(a.Name)},
	}
}

func (t *renameStageTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a renameStageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	stageID, err := knownID(a.StageID, "stage_id", "list_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	name := strings.TrimSpace(a.Name)
	if _, err := t.deps.UpdateStage.Execute(cc.WorkspaceID, stageID, stage.UpdateStageInput{Name: &name}); err != nil {
		return funnelFailure("rename_stage", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"renamed": true}}
}

type reorderStagesArgs struct {
	PipelineID string   `json:"pipeline_id" req:"true" desc:"pipeline_id exato de list_pipelines" id:"true"`
	StageIDs   []string `json:"stage_ids" req:"true" desc:"todos os stage_id do funil, na nova ordem" id:"true"`
}

type reorderStagesTool struct{ deps FunnelDeps }

func NewReorderStagesTool(deps FunnelDeps) copilot.Tool { return &reorderStagesTool{deps: deps} }

func (t *reorderStagesTool) Meta() copilot.Meta { return stagesMeta(workspace.ActionUpdate) }

func (t *reorderStagesTool) Definition() tools.Definition {
	return definition("reorder_stages", "Muda a ordem das etapas de um funil. Só depois da aprovação do usuário.", reorderStagesArgs{})
}

func (t *reorderStagesTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a reorderStagesArgs
	bindArgs(args, &a)
	names := namesOf(t.deps, cc)
	order := make([]string, 0, len(a.StageIDs))
	for _, id := range a.StageIDs {
		order = append(order, names.stage(strings.TrimSpace(id)))
	}
	return []copilot.Field{
		{Key: "pipeline", Value: names.pipeline(strings.TrimSpace(a.PipelineID))},
		{Key: "order", Value: strings.Join(order, ", ")},
	}
}

func (t *reorderStagesTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a reorderStagesArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	pipelineID, err := knownID(a.PipelineID, "pipeline_id", "list_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	ids := make([]string, 0, len(a.StageIDs))
	for _, raw := range a.StageIDs {
		id, err := knownID(raw, "stage_ids", "list_pipelines")
		if err != nil {
			return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
		}
		ids = append(ids, id)
	}
	if _, err := t.deps.ReorderStages.Execute(cc.WorkspaceID, stage.ReorderStagesInput{StageIDs: ids, PipelineID: pipelineID}); err != nil {
		return funnelFailure("reorder_stages", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"reordered": true}}
}

type setInitialStageArgs struct {
	StageID string `json:"stage_id" req:"true" desc:"stage_id exato de list_pipelines" id:"true"`
}

type setInitialStageTool struct{ deps FunnelDeps }

func NewSetInitialStageTool(deps FunnelDeps) copilot.Tool { return &setInitialStageTool{deps: deps} }

func (t *setInitialStageTool) Meta() copilot.Meta { return stagesMeta(workspace.ActionUpdate) }

func (t *setInitialStageTool) Definition() tools.Definition {
	return definition("set_initial_stage", "Define a etapa em que as conversas novas entram no funil. Só depois da aprovação do usuário.", setInitialStageArgs{})
}

func (t *setInitialStageTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a setInitialStageArgs
	bindArgs(args, &a)
	return []copilot.Field{{Key: "stage", Value: namesOf(t.deps, cc).stage(strings.TrimSpace(a.StageID))}}
}

func (t *setInitialStageTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a setInitialStageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	stageID, err := knownID(a.StageID, "stage_id", "list_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if _, err := t.deps.SetInitial.Execute(cc.WorkspaceID, stageID); err != nil {
		return funnelFailure("set_initial_stage", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"done": true}}
}

func funnelFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, stage.ErrUnauthorized), errors.Is(err, pipeline.ErrUnauthorized):
		return copilot.Result{Status: copilot.StatusDenied, Message: "esse funil ou etapa não é deste workspace"}
	case errors.Is(err, stage.ErrTagNotFound), errors.Is(err, pipeline.ErrNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "funil ou etapa desconhecido; use os ids de list_pipelines"}
	case errors.Is(err, stage.ErrTagNameExists):
		return copilot.Result{Status: copilot.StatusError, Message: "já existe uma etapa com esse nome neste funil"}
	case errors.Is(err, stage.ErrTagNameRequired), errors.Is(err, pipeline.ErrNameRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "o nome é obrigatório"}
	case errors.Is(err, stage.ErrTagDescRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "a descrição da etapa é obrigatória"}
	case errors.Is(err, stage.ErrTagDefaultUpdate):
		return copilot.Result{Status: copilot.StatusError, Message: "essa etapa padrão não pode ser editada"}
	case errors.Is(err, pipeline.ErrDepartmentUnknown):
		return copilot.Result{Status: copilot.StatusError, Message: "departamento desconhecido; use os ids de list_departments"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao mudar o funil"}
}

func (t *createPipelineTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[createPipelineArgs](nil, cc, args)
	return err
}

func (t *createStageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[createStageArgs](nil, cc, args)
	return err
}

func (t *renameStageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[renameStageArgs](nil, cc, args)
	return err
}

func (t *reorderStagesTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[reorderStagesArgs](nil, cc, args)
	return err
}

func (t *setInitialStageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[setInitialStageArgs](nil, cc, args)
	return err
}
