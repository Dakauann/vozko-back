package ws

import (
	"encoding/json"
	"log"

	ca "vozko/domain/comment_analysis"
)

// The live comment feed's transport (§7).
//
// Scoping is the whole content of this file. A comment-analysis event is not
// about a conversation entry, so `CanAccessEntry` says nothing useful about it;
// what governs it is the same permission the REST reads carry, asked of the
// authorizer the hub already holds. No second RBAC implementation, and no
// broadcast to a connection whose user may not read the feature.
//
// Delivery is best effort by contract: a full send buffer drops the event
// rather than blocking, because the row is already stored and the viewer will
// see it on their next read.

// BroadcastCommentsAnalyzed implements ca.AnalysisBroadcaster.
func (h *ConversationHub) BroadcastCommentsAnalyzed(event ca.AnalysisBatchAnalyzed) {
	if h == nil || event.WorkspaceID == "" || len(event.Items) == 0 {
		return
	}
	data, err := json.Marshal(&WSOutgoingMessage{
		Type:    WSEventCommentAnalysisAnalyzed,
		Payload: event,
	})
	if err != nil {
		log.Printf("[ConversationHub] comment analysis broadcast: %v", err)
		return
	}
	h.sendToWorkspaceWithPermission(event.WorkspaceID, "comment_analysis", "read", data)
	// The other replicas hold the rest of this workspace's viewers. The event
	// travels whole because there is nothing on the far side to rebuild it
	// from, unlike the entry-shaped broadcasts next door.
	h.publishWorkspacePayload("comment_analysis_analyzed", event.WorkspaceID, data)
}

// sendToWorkspaceWithPermission fans one already-marshalled frame out to every
// connection of a workspace whose user holds the given permission.
func (h *ConversationHub) sendToWorkspaceWithPermission(workspaceID, resource, action string, data []byte) {
	if workspaceID == "" || len(data) == 0 || h.authorizer == nil {
		return
	}
	h.connMu.RLock()
	defer h.connMu.RUnlock()

	for connID, conn := range h.connections {
		if conn == nil || conn.WorkspaceID != workspaceID {
			continue
		}
		if !h.authorizer.HasWorkspacePermission(conn.UserID, conn.WorkspaceID, resource, action, conn.IsAdmin) {
			continue
		}
		select {
		case conn.Send <- data:
		default:
			// Dropped, not blocked: the row is stored, and a viewer whose
			// buffer is full is behind on everything anyway.
			log.Printf("[ConversationHub] Send buffer full for user %s (connection %s)", conn.UserID, connID)
		}
	}
}

// publishWorkspacePayload is publishWorkspaceBroadcast for the kinds that carry
// their own body rather than being rebuilt on the receiving replica.
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
