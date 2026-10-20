package conversation_usecase

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type FinishAccess interface {
	shared.EntryAccessChecker
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
}

type personFinish struct {
	access FinishAccess
	status conversation.ConversationStatusUpdater
}

func NewPersonFinish(access FinishAccess, status conversation.ConversationStatusUpdater) conversation.PersonFinishUseCase {
	return &personFinish{access: access, status: status}
}

func (uc *personFinish) Finish(_ context.Context, by shared.Person, workspaceID string, entry shared.EntryRef, outcomeCode string) error {
	if !by.MayActOn(uc.access, workspaceID, entry.EntryID, string(entry.EntryType)) {
		return conversation.ErrUnauthorized
	}
	if !uc.access.HasWorkspacePermission(by.UserID, workspaceID, string(workspace.ResourceConversations), string(workspace.ActionSend), by.SystemAdmin) {
		return conversation.ErrStatusChangeForbidden
	}
	return uc.status.Finish(entry.EntryID, string(entry.EntryType), conversation.FinishOptions{
		Source:      conversation.CloseSourceHuman,
		Reason:      conversation.CloseReasonManual,
		ActorID:     by.UserID,
		OutcomeCode: outcomeCode,
	})
}
