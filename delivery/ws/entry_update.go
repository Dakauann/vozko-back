package ws

import (
	"encoding/json"
	"log"

	"vozko/domain/conversation"
)

type entryUpdate struct {
	entryID     string
	entryType   string
	workspaceID string
	campaignID  string
	entry       *conversation.InboxEntry
	data        []byte
}

func (h *ConversationHub) entryWorkspaceID(entryID, entryType string) string {
	if h.workspaceResolver == nil {
		return ""
	}
	workspaceID, _ := h.workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
	return workspaceID
}

func (h *ConversationHub) buildEntryUpdate(entryID, entryType, workspaceID string, silent bool) (*entryUpdate, bool) {
	var entry *conversation.InboxEntry
	var err error
	switch {
	case h.inboxService != nil:
		entry, err = h.inboxService.BuildInboxEntry(entryID, entryType)
	case h.historyProvider != nil:
		entry, err = h.historyProvider.GetInboxEntry(entryID, entryType)
	default:
		return nil, false
	}
	if err != nil {
		log.Printf("[ConversationHub] Error building inbox entry %s (%s): %v", entryID, entryType, err)
		return nil, false
	}
	if entry == nil {
		log.Printf("[ConversationHub] BroadcastEntryUpdate: entry %s (%s) not found", entryID, entryType)
		return nil, false
	}

	var campaignID string
	if h.workspaceResolver != nil {
		campaignID, _ = h.workspaceResolver.GetEntryCampaignID(entryID, entryType)
	}

	data, err := json.Marshal(&WSOutgoingMessage{
		Type:    WSEventEntryUpdate,
		Payload: EntryUpdatePayload{Entry: *entry, Silent: silent},
	})
	if err != nil {
		log.Printf("[ConversationHub] Error marshaling entry update: %v", err)
		return nil, false
	}
	return &entryUpdate{
		entryID: entryID, entryType: entryType, workspaceID: workspaceID,
		campaignID: campaignID, entry: entry, data: data,
	}, true
}

func (h *ConversationHub) entryUpdateReaches(update *entryUpdate, allowEntry func(*WSConnection) bool, conn *WSConnection) bool {
	if !allowEntry(conn) {
		return false
	}
	if update.workspaceID != "" && conn.WorkspaceID != "" && conn.WorkspaceID != update.workspaceID {
		return false
	}
	if update.campaignID != "" && conn.CampaignID != "" && conn.CampaignID != update.campaignID {
		return false
	}
	if conn.CampaignType != "" && conn.CampaignType != update.entryType {
		return false
	}
	if conn.ConversationStatus != "" && string(update.entry.ConversationStatus) != conn.ConversationStatus {
		return false
	}
	assignedUserID := update.entry.AssignedUserID
	if assignedUserID != "" && conn.UserID != assignedUserID && !conn.IsAdmin {
		return h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, "conversations", "view_others", conn.IsAdmin)
	}
	return true
}

func (h *ConversationHub) deliverEntryUpdate(conn *WSConnection, update *entryUpdate) {
	select {
	case conn.Send <- update.data:
	default:
		log.Printf("[ConversationHub] Send buffer full for user %s (connection %s)", conn.UserID, conn.ID)
	}
}
