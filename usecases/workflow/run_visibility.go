package workflow_usecase

import (
	"log/slog"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func runVisibleTo(visibility conversation.Visibility, viewer shared.Person, run *workflow.WorkflowRun) bool {
	if !run.OnConversation() {
		return true
	}
	return conversationVisibleTo(visibility, viewer, run.WorkspaceID, run.EntryID, run.EntryType) == nil
}

func conversationVisibleTo(visibility conversation.Visibility, viewer shared.Person, workspaceID, entryID, entryType string) error {
	err := conversation.RequireVisible(visibility, viewer, workspaceID, entryID, entryType)
	if err != nil && err != conversation.ErrNotVisible {
		slog.Warn("workflow.conversation.visibility_unreadable",
			slog.String("workspace_id", workspaceID), slog.String("entry_id", entryID), slog.String("error", err.Error()))
	}
	return err
}

func findVisibleRun(runRepo workflow.WorkflowRunRepository, visibility conversation.Visibility, viewer shared.Person, workspaceID, runID string) (*workflow.WorkflowRun, error) {
	run, err := findWorkspaceRun(runRepo, workspaceID, runID)
	if err != nil {
		return nil, err
	}
	if !runVisibleTo(visibility, viewer, run) {
		return nil, workflow.ErrRunNotFound
	}
	return run, nil
}

func conversationScope(visibility conversation.Visibility, viewer shared.Person, workspaceID string) conversation.VisibilityScope {
	if visibility == nil {
		return conversation.NothingVisible()
	}
	scope, err := visibility.Scope(viewer, workspaceID)
	if err != nil {
		slog.Info("workflow.conversation_scope_unreadable",
			slog.String("workspace_id", workspaceID), slog.String("error", err.Error()))
		return conversation.NothingVisible()
	}
	return scope
}

type ListConversationRunsUseCase struct {
	runs       workflow.ConversationRuns
	visibility conversation.Visibility
	steps      StepNamer
}

func NewListConversationRunsUseCase(runs workflow.ConversationRuns, visibility conversation.Visibility, steps StepNamer) *ListConversationRunsUseCase {
	return &ListConversationRunsUseCase{runs: runs, visibility: visibility, steps: steps}
}

func (uc *ListConversationRunsUseCase) Execute(viewer shared.Person, workspaceID, entryType, entryID string, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.ConversationRun], error) {
	if strings.TrimSpace(entryType) == "" || strings.TrimSpace(entryID) == "" {
		return nil, conversation.ErrNotVisible
	}
	if err := conversationVisibleTo(uc.visibility, viewer, workspaceID, entryID, entryType); err != nil {
		return nil, err
	}
	page, err := uc.runs.ListByEntry(workspaceID, entryID, entryType, pagination)
	if err != nil || page == nil {
		return page, err
	}
	described := *page
	described.Items = make([]*workflow.ConversationRun, len(page.Items))
	for i, run := range page.Items {
		item := run.Described(uc.steps)
		described.Items[i] = &item
	}
	return &described, nil
}
