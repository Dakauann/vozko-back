package ws

import (
	"encoding/json"
	"log"

	"vozko/delivery/notificationview"
	"vozko/domain/notification"
)

const notificationCreatedBroadcastType = "notification_created"

var _ notification.Pusher = (*ConversationHub)(nil)

func (h *ConversationHub) PushNotification(workspaceID string, userIDs []string, item notification.Item) {
	if h == nil || workspaceID == "" || len(userIDs) == 0 {
		return
	}
	data, err := json.Marshal(&WSOutgoingMessage{
		Type:    WSEventNotificationCreated,
		Payload: notificationview.Created{WorkspaceID: workspaceID, Item: notificationview.ItemOf(item)},
	})
	if err != nil {
		log.Printf("[ConversationHub] notification push: %v", err)
		return
	}
	h.sendToUsers(workspaceID, userIDs, data)
	h.publishUserPayload(notificationCreatedBroadcastType, workspaceID, userIDs, data)
}

func (h *ConversationHub) sendToUsers(workspaceID string, userIDs []string, data []byte) {
	recipients := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		recipients[id] = true
	}
	h.sendToWorkspaceWhere(workspaceID, data, func(_ string, conn *WSConnection) bool {
		return recipients[conn.UserID]
	})
}

func (h *ConversationHub) publishUserPayload(bType, workspaceID string, userIDs []string, payload []byte) {
	if h.sharedState == nil {
		return
	}
	data, err := json.Marshal(redisWorkspaceBroadcast{
		Type:        bType,
		WorkspaceID: workspaceID,
		UserIDs:     userIDs,
		Payload:     payload,
		ReplicaID:   h.replicaID,
	})
	if err != nil {
		return
	}
	_ = h.sharedState.Publish("hub:workspace_broadcast", data)
}
