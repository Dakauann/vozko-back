package crmtools

import (
	"context"
	"errors"
	"log"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/stage"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
)

type StageBroadcaster interface {
	BroadcastStageUpdate(workspaceID, entryID, entryType string)
}

type StageMoveDeps struct {
	Move      stage.MoveEntryStageUseCase
	Funnels   stage.ListFunnelStagesUseCase
	Entries   conversation.EntryLookup
	Broadcast StageBroadcaster
}

type moveStageArgs struct {
	EntryID   string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType string `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
	StageID   string `json:"stage_id" req:"true" desc:"stage_id exato de list_pipelines" id:"true"`
}

type moveStageTool struct {
	deps        StageMoveDeps
	crossFunnel bool
}

func NewMoveConversationStageTool(deps StageMoveDeps) copilot.Tool {
	return &moveStageTool{deps: deps}
}

func NewMoveConversationFunnelTool(deps StageMoveDeps) copilot.Tool {
	return &moveStageTool{deps: deps, crossFunnel: true}
}

func (t *moveStageTool) Meta() copilot.Meta {
	action := workspace.ActionAssign
	if t.crossFunnel {
		action = workspace.ActionTransfer
	}
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceStages, Action: action}
}

func (t *moveStageTool) Definition() tools.Definition {
	if t.crossFunnel {
		return copilottools.Definition("move_conversation_funnel",
			"Move uma conversa para uma etapa de OUTRO funil (tira do quadro de uma equipe e coloca no de outra). "+
				"Só depois da aprovação do usuário.", moveStageArgs{})
	}
	return copilottools.Definition("move_conversation_stage",
		"Move uma conversa para outra etapa do mesmo funil. Só depois da aprovação do usuário. "+
			"Para uma etapa de outro funil, use move_conversation_funnel.", moveStageArgs{})
}

func (t *moveStageTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a moveStageArgs
	copilottools.BindArgs(args, &a)
	return []copilot.Field{
		{Key: "conversation", Value: copilottools.DescribeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)},
		{Key: "stage", Value: t.stageName(cc, a.StageID)},
	}
}

func (t *moveStageTool) stageName(cc copilot.Context, stageID string) string {
	funnels, err := t.deps.Funnels.Execute(cc.WorkspaceID)
	if err != nil {
		return "etapa desconhecida"
	}
	for _, f := range funnels {
		for _, s := range f.Stages {
			if s != nil && s.ID == stageID {
				return s.Name + " (" + f.PipelineName + ")"
			}
		}
	}
	return "etapa desconhecida"
}

func (t *moveStageTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a moveStageArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := copilottools.TargetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	stageID, err := copilottools.KnownID(a.StageID, "stage_id", "list_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	moved, err := t.deps.Move.Execute(cc.WorkspaceID, copilottools.PersonOf(cc), stage.AssignEntryStageInput{
		StageID:            stageID,
		EntryID:            target.EntryID,
		EntryType:          string(target.EntryType),
		AllowCrossPipeline: t.crossFunnel,
	})
	if err != nil {
		return stageMoveFailure(t.Definition().Name, err)
	}
	if t.deps.Broadcast != nil {
		t.deps.Broadcast.BroadcastStageUpdate(cc.WorkspaceID, target.EntryID, string(target.EntryType))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"moved": true, "stage": t.stageName(cc, moved.StageID),
	}}
}

func stageMoveFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, stage.ErrEntryAccess), errors.Is(err, stage.ErrUnauthorized):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta conversa ou etapa"}
	case errors.Is(err, stage.ErrStagePipelineMismatch):
		return copilot.Result{Status: copilot.StatusError, Message: "essa etapa é de outro funil; para trocar de funil use move_conversation_funnel"}
	case errors.Is(err, stage.ErrTagNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "etapa desconhecida; use os ids de list_pipelines"}
	case errors.Is(err, stage.ErrInvalidEntryType):
		return copilot.Result{Status: copilot.StatusError, Message: "este tipo de conversa não tem etapas"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao mover a conversa"}
}

func (t *moveStageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := copilottools.ValidateArgs[moveStageArgs](t.deps.Entries, cc, args)
	return err
}
