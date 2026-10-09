package ws

import (
	"encoding/json"
	"log"

	ca "vozko/domain/audience"
	ld "vozko/domain/livedecision"
)

func (h *ConversationHub) BroadcastCommentsAnalyzed(event ca.AnalysisBatchAnalyzed) {
	if h == nil || event.WorkspaceID == "" || len(event.Items) == 0 {
		return
	}
	data, err := json.Marshal(&WSOutgoingMessage{
		Type:    WSEventAudienceAnalyzed,
		Payload: event,
	})
	if err != nil {
		log.Printf("[ConversationHub] comment analysis broadcast: %v", err)
		return
	}
	h.sendToWorkspaceWithPermission(event.WorkspaceID, "audience", "read", data)
	h.publishWorkspacePayload("audience_analyzed", event.WorkspaceID, data)
}

func (h *ConversationHub) sendToWorkspaceWithPermission(workspaceID, resource, action string, data []byte) {
	if h.authorizer == nil {
		return
	}
	h.sendToWorkspaceWhere(workspaceID, data, func(_ string, conn *WSConnection) bool {
		return h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, resource, action, conn.IsAdmin)
	})
}

func (h *ConversationHub) sendToWorkspaceWhere(workspaceID string, data []byte, allow func(connID string, conn *WSConnection) bool) {
	if workspaceID == "" || len(data) == 0 {
		return
	}
	h.connMu.RLock()
	defer h.connMu.RUnlock()

	for connID, conn := range h.connections {
		if conn == nil || conn.WorkspaceID != workspaceID || !allow(connID, conn) {
			continue
		}
		select {
		case conn.Send <- data:
		default:
			log.Printf("[ConversationHub] Send buffer full for user %s (connection %s)", conn.UserID, connID)
		}
	}
}

func (h *ConversationHub) publishWorkspacePayload(bType, workspaceID string, payload []byte) {
	if h.sharedState == nil {
		return
	}
	data, err := json.Marshal(redisWorkspaceBroadcast{
		Type:        bType,
		WorkspaceID: workspaceID,
		Payload:     payload,
		ReplicaID:   h.replicaID,
	})
	if err != nil {
		return
	}
	_ = h.sharedState.Publish("hub:workspace_broadcast", data)
}

func (h *ConversationHub) AnalysisStateChanged(state ca.ConversationAnalysisState) {
	if h == nil || state.EntryID == "" || state.EntryType == "" {
		return
	}
	var analysis interface{}
	if state.Analysis != nil {
		analysis = state.Analysis
	}
	h.broadcast <- &broadcastMessage{
		entryID:   state.EntryID,
		entryType: state.EntryType,
		event: &WSOutgoingMessage{
			Type: WSEventAnalysisUpdate,
			Payload: AnalysisUpdatePayload{
				EntryID:   state.EntryID,
				EntryType: state.EntryType,
				Analysis:  analysis,
				Pending:   state.Pending,
			},
		},
	}
}

func (h *ConversationHub) LiveReadChanged(read ld.LiveRead) {
	if h == nil || read.EntryID == "" || read.EntryType == "" || !read.HasLabels() {
		return
	}
	go h.RefreshEntry(read.EntryID, read.EntryType)
}
