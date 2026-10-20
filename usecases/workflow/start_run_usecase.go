package workflow_usecase

import (
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

type conversationStarter interface {
	StartForConversation(in ConversationStart) (StartedRun, error)
}

type startRunUseCase struct {
	starter    conversationStarter
	runs       workflow.WorkflowRunRepository
	visibility conversation.Visibility
}

func NewStartRunUseCase(starter *ConversationRunStarter, runs workflow.WorkflowRunRepository, visibility conversation.Visibility) workflow.StartRunUseCase {
	return &startRunUseCase{starter: starter, runs: runs, visibility: visibility}
}

func (uc *startRunUseCase) Execute(input workflow.StartRunInput) (*workflow.WorkflowRun, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.WorkflowID) == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	if err := conversationVisibleTo(uc.visibility, input.Viewer, input.WorkspaceID, input.EntryID, input.EntryType); err != nil {
		return nil, err
	}
	data := make(map[string]interface{}, len(input.Variables)+1)
	for k, v := range input.Variables {
		data[k] = v
	}
	data[workflow.VarChannel] = input.EntryType
	started, err := uc.starter.StartForConversation(ConversationStart{
		WorkspaceID: input.WorkspaceID,
		WorkflowID:  input.WorkflowID,
		EntryID:     input.EntryID,
		EntryType:   input.EntryType,
		StartedBy:   input.StartedBy,
		Data:        data,
	})
	if err != nil {
		return nil, err
	}
	if started.AlreadyRunning {
		return nil, workflow.ErrRunAlreadyExists
	}
	return findWorkspaceRun(uc.runs, input.WorkspaceID, started.RunID)
}
