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

// operatorSendFinalizer applies the four side effects every delivered human
// reply owes its conversation.
//
// Lifted out of ConversationHub.afterOperatorSend. While it lived in the
// delivery layer only the WebSocket composer ran it, so the HTTP send endpoint
// and, later, the scheduled dispatcher would each have had to re-derive the
// same steps or silently skip them.
type operatorSendFinalizer struct {
	statusUpdater     conversation.ConversationStatusUpdater
	workspaceResolver conversation.CampaignWorkspaceResolver
	events            ce.Logger
	aiSessions        AISessionEnder
	initialStage      conversation.InitialStageAssigner
}

// NewOperatorSendFinalizer wires the finalizer.
//
// Every dependency is required and checked here rather than at each use: a nil
// one does not fail a send, it silently drops a conversation off the board or
// out of the timeline, and that class of bug is invisible until someone asks why
// a channel's conversations never close. Failing at container-wiring time turns
// it into a deployment error instead.
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

// FinalizeOperatorSend runs the four steps in the order the hub ran them.
//
// Individual steps degrade to a log line rather than an error: a conversation
// must not lose a delivered message because its telemetry publisher is briefly
// unreachable. Only a malformed input is refused.
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
	// Every entry type names its own channel. This was a switch that listed
	// three of them and defaulted to "whatsapp", so an operator's Telegram
	// reply was recorded as a WhatsApp event.
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

// resolveWorkspace prefers the resolver's answer over the caller's hint: the
// hint is the connection's workspace, which is right for the common case and
// wrong for a platform admin working across workspaces.
func (f *operatorSendFinalizer) resolveWorkspace(entryID, entryType, hint string) string {
	resolved, err := f.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	if err != nil || resolved == "" {
		return hint
	}
	return resolved
}

// ensureInitialStage puts a freshly-replied entry on the board.
//
// Both lookups are required and both are logged when empty: an entry with no
// campaign cannot be staged, and a silent return here is what leaves a
// conversation invisible on the kanban.
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
