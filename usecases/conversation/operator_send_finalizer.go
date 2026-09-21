package conversation_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/shared"
)

type operatorSendFinalizer struct {
	statusUpdater     conversation.ConversationStatusUpdater
	workspaceResolver conversation.CampaignWorkspaceResolver
	events            ce.Logger
	aiSessions        AISessionEnder
	initialStage      conversation.InitialStageAssigner
}

func NewOperatorSendFinalizer(
	statusUpdater conversation.ConversationStatusUpdater,
	workspaceResolver conversation.CampaignWorkspaceResolver,
	events ce.Logger,
	aiSessions AISessionEnder,
	initialStage conversation.InitialStageAssigner,
) (conversation.OperatorSendFinalizer, error) {
	missing := []string{}
	if statusUpdater == nil {
		missing = append(missing, "conversation status updater")
	}
	if workspaceResolver == nil {
		missing = append(missing, "campaign workspace resolver")
	}
	if events == nil {
		missing = append(missing, "conversation event logger")
	}
	if aiSessions == nil {
		missing = append(missing, "ai session ender")
	}
	if initialStage == nil {
		missing = append(missing, "initial stage assigner")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("operator send finalizer: missing %s", strings.Join(missing, ", "))
	}

	return &operatorSendFinalizer{
		statusUpdater:     statusUpdater,
		workspaceResolver: workspaceResolver,
		events:            events,
		aiSessions:        aiSessions,
		initialStage:      initialStage,
	}, nil
}

func (f *operatorSendFinalizer) FinalizeOperatorSend(_ context.Context, in conversation.FinalizeOperatorSendInput) error {
	entryID := strings.TrimSpace(in.EntryID)
	entryType := strings.TrimSpace(in.EntryType)
	if entryID == "" {
		return conversation.ErrEntryIDRequired
	}
	if entryType == "" {
		return conversation.ErrEntryTypeInvalid
	}

	if in.Message != nil {
		if err := f.statusUpdater.TransitionOnMessage(
			entryID, entryType, in.Message.MessageType, in.Message.ResolvedDirection(),
		); err != nil {
			log.Printf("[OperatorSend] status transition failed for %s (%s): %v", entryID, entryType, err)
		}
	}

	workspaceID := f.resolveWorkspace(entryID, entryType, in.WorkspaceID)

	details := map[string]string{}
	if in.Message != nil && in.Message.ID != "" {
		details["message_id"] = in.Message.ID
	}
	f.events.Log(ce.New(workspaceID, entryID, entryType, ce.EventReplied).
		WithActorHuman(in.ActorUserID).
		WithChannel(shared.EntryType(entryType).EventChannel()).
		WithDetails(details).
		Build())

	if workspaceID != "" {
		f.aiSessions.EndOpenRaw(workspaceID, entryID, entryType, "handed_off", "human_reply", in.ActorUserID)
	}

	f.ensureInitialStage(workspaceID, entryID, entryType)
	return nil
}

func (f *operatorSendFinalizer) resolveWorkspace(entryID, entryType, hint string) string {
	resolved, err := f.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil || resolved == "" {
		return hint
	}
	return resolved
}

func (f *operatorSendFinalizer) ensureInitialStage(workspaceID, entryID, entryType string) {
	if workspaceID == "" {
		log.Printf("[OperatorSend] ensureInitialStage: empty workspaceID for %s (%s)", entryID, entryType)
		return
	}
	campaignID, err := f.workspaceResolver.GetEntryCampaignID(entryID, entryType)
	if err != nil || campaignID == "" {
		log.Printf("[OperatorSend] ensureInitialStage: empty campaignID for %s (%s): %v", entryID, entryType, err)
		return
	}
	f.initialStage.AutoAssignInitialStage(workspaceID, campaignID, entryType, entryID, entryType)
}

var _ conversation.OperatorSendFinalizer = (*operatorSendFinalizer)(nil)
