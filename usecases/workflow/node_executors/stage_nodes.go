package node_executors

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/stage"
	"vozko/domain/workflow"
)

// StageReader reads a stage and the stage a conversation is in.
type StageReader interface {
	FindByID(id string) (*stage.Stage, error)
	GetEntryStage(entryID, entryType, workspaceID string) (*stage.EntryStage, error)
}

// StageBroadcaster tells open screens a conversation changed stage.
type StageBroadcaster interface {
	BroadcastStageUpdate(workspaceID, entryID, entryType string)
}

// stagePicker is the stage field both stage nodes share: every stage of the
// workspace's pipelines, labelled with its pipeline.
func stagePicker(description string) workflow.ConfigField {
	return workflow.ConfigField{
		Key:           "stage_id",
		Label:         "Etapa",
		Type:          "select",
		OptionsSource: "stages",
		Required:      true,
		Description:   description,
	}
}

// currentStage is the stage the conversation is in, nil when it is in none.
func currentStage(stages StageReader, run *workflow.WorkflowRun) *stage.EntryStage {
	current, err := stages.GetEntryStage(run.EntryID, run.EntryType, run.WorkspaceID)
	if err != nil || current == nil || current.StageID == "" {
		return nil
	}
	return current
}

type moveStageExecutor struct {
	stages    StageReader
	mover     stage.AssignEntryStageUseCase
	broadcast StageBroadcaster
}

// NewMoveStageExecutor moves the conversation through the same use case people
// and the AI use, so the pipeline rule and the timeline entry apply.
func NewMoveStageExecutor(stages StageReader, mover stage.AssignEntryStageUseCase, broadcast StageBroadcaster) workflow.NodeExecutor {
	return &moveStageExecutor{stages: stages, mover: mover, broadcast: broadcast}
}

func (e *moveStageExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionMoveStage,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Mover Etapa",
		Description: "Move a conversa para uma etapa do funil.",
		Icon:        "Kanban",
		Guidance: workflow.NodeGuidance{
			When: "Para avançar a conversa no funil conforme o fluxo (ex.: qualificado, proposta enviada, ganho).",
			Behavior: "Segue a mesma regra de quem move pelo CRM: a conversa só muda de etapa dentro do próprio funil; " +
				"uma conversa ainda sem etapa entra no funil da etapa escolhida. Já estando na etapa, não muda nada.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a conversa está na etapa escolhida"},
			{Key: "changed", Description: "true quando a conversa mudou de etapa (false se já estava nela)"},
			{Key: "stage_id", Description: "ID da etapa escolhida"},
			{Key: "stage_name", Description: "Nome da etapa escolhida"},
			{Key: "pipeline_id", Description: "ID do funil da etapa"},
			{Key: "from_stage_id", Description: "ID da etapa anterior (vazio se não havia)"},
			{Key: "from_stage_name", Description: "Nome da etapa anterior"},
			{Key: "error", Description: "Descrição do erro quando a conversa não pôde ser movida"},
		},
		DefaultConfig: map[string]interface{}{"stage_id": ""},
		ConfigSchema:  []workflow.ConfigField{stagePicker("A etapa para onde a conversa vai.")},
	}
}

func (e *moveStageExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	stageID, _ := ctx.Node.Config["stage_id"].(string)
	stageID = strings.TrimSpace(workflow.Interpolate(stageID, ctx.State, nil))
	if stageID == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	if e.stages == nil || e.mover == nil {
		return failMove(edges, stageID, "mover etapa indisponível neste contexto (ex.: simulação)"), nil
	}

	target, err := e.stages.FindByID(stageID)
	if err != nil || target == nil || target.WorkspaceID != ctx.Run.WorkspaceID {
		return failMove(edges, stageID, "a etapa escolhida não existe mais neste workspace: escolha outra no campo Etapa do nó"), nil
	}

	out := map[string]interface{}{
		"success":     true,
		"changed":     false,
		"stage_id":    target.ID,
		"stage_name":  target.Name,
		"pipeline_id": target.PipelineID,
	}
	from := currentStage(e.stages, ctx.Run)
	if from != nil {
		out["from_stage_id"], out["from_stage_name"] = from.StageID, from.StageName
		if from.StageID == target.ID {
			return &workflow.NodeResult{NextNodeID: resolveEdgeByLabel(edges, "sucesso"), Output: out}, nil
		}
	}

	if _, err := e.mover.Execute(ctx.Run.WorkspaceID, stage.AssignEntryStageInput{
		StageID:   target.ID,
		EntryID:   ctx.Run.EntryID,
		EntryType: ctx.Run.EntryType,
		ActorID:   workflowActor(ctx.Run.WorkflowID),
	}); err != nil {
		return failMove(edges, stageID, moveFailure(err)), nil
	}
	if e.broadcast != nil {
		e.broadcast.BroadcastStageUpdate(ctx.Run.WorkspaceID, ctx.Run.EntryID, ctx.Run.EntryType)
	}

	out["changed"] = true
	return &workflow.NodeResult{NextNodeID: resolveEdgeByLabel(edges, "sucesso"), Output: out}, nil
}

func failMove(edges []workflow.Edge, stageID, reason string) *workflow.NodeResult {
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabel(edges, "erro"),
		Output: map[string]interface{}{
			"success":  false,
			"changed":  false,
			"stage_id": stageID,
			"error":    reason,
		},
	}
}

// moveFailure says in the builder's words why the conversation did not move.
func moveFailure(err error) string {
	if errors.Is(err, stage.ErrStagePipelineMismatch) {
		return "a etapa é de outro funil: a conversa só muda de etapa dentro do próprio funil"
	}
	return fmt.Sprintf("falha ao mover a conversa de etapa: %v", err)
}

type checkStageExecutor struct {
	stages StageReader
}

func NewCheckStageExecutor(stages StageReader) workflow.NodeExecutor {
	return &checkStageExecutor{stages: stages}
}

func (e *checkStageExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeConditionCheckStage,
		Category:    workflow.NodeCategoryCondition,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Verificar Etapa",
		Description: "Verifica se a conversa está em uma etapa do funil.",
		Icon:        "Kanban",
		Outputs: []workflow.HandleDefinition{
			{ID: "true", Label: "Está na etapa"},
			{ID: "false", Label: "Não está"},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "matched", Description: "true se a conversa está na etapa escolhida"},
			{Key: "stage_id", Description: "ID da etapa verificada"},
			{Key: "current_stage_id", Description: "ID da etapa atual da conversa (vazio se não há)"},
			{Key: "current_stage_name", Description: "Nome da etapa atual da conversa"},
		},
		DefaultConfig: map[string]interface{}{"stage_id": ""},
		ConfigSchema:  []workflow.ConfigField{stagePicker("A etapa a comparar com a etapa atual da conversa.")},
	}
}

func (e *checkStageExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	stageID, _ := ctx.Node.Config["stage_id"].(string)
	stageID = strings.TrimSpace(workflow.Interpolate(stageID, ctx.State, nil))
	if stageID == "" {
		return nil, workflow.ErrNodeConfigMissing
	}

	out := map[string]interface{}{"matched": false, "stage_id": stageID, "current_stage_id": "", "current_stage_name": ""}
	var current *stage.EntryStage
	if e.stages != nil {
		current = currentStage(e.stages, ctx.Run)
	}
	if current != nil {
		out["current_stage_id"], out["current_stage_name"] = current.StageID, current.StageName
		out["matched"] = current.StageID == stageID
	}

	branch := "false"
	if out["matched"] == true {
		branch = "true"
	}
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabelStrict(ctx.Graph.OutgoingEdges(ctx.Node.ID), branch),
		Output:     out,
	}, nil
}
