package ws

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/notification"
)

func connectionFor(userID, workspaceID string) *WSConnection {
	return &WSConnection{UserID: userID, WorkspaceID: workspaceID, Send: make(chan []byte, 4)}
}

func notificationHub(conns map[string]*WSConnection) *ConversationHub {
	return &ConversationHub{connections: conns}
}

func TestNotificationPushReachesOnlyRecipientsOfThatWorkspace(t *testing.T) {
	recipient := connectionFor("u-1", "ws-1")
	sameUserElsewhere := connectionFor("u-1", "ws-2")
	colleague := connectionFor("u-2", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": recipient, "b": sameUserElsewhere, "c": colleague})

	item := notification.Item{ID: "n-1", Category: notification.CategoryCampaigns, Severity: notification.SeverityWarning, Title: "Pausada", CreatedAt: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	hub.PushNotification("ws-1", []string{"u-1"}, item)

	select {
	case raw := <-recipient.Send:
		var msg struct {
			Type    string `json:"type"`
			Payload struct {
				WorkspaceID string `json:"workspaceId"`
				Item        struct {
					ID       string `json:"id"`
					Category string `json:"category"`
				} `json:"item"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type != "notification:created" || msg.Payload.WorkspaceID != "ws-1" || msg.Payload.Item.ID != "n-1" || msg.Payload.Item.Category != "campaigns" {
			t.Fatalf("message = %+v", msg)
		}
	default:
		t.Fatal("the recipient must receive the push")
	}
	if len(sameUserElsewhere.Send) != 0 || len(colleague.Send) != 0 {
		t.Fatal("only the recipient's connection in that workspace receives it")
	}
}

func TestNotificationPushFromAnotherReplicaIsDeliveredLocally(t *testing.T) {
	recipient := connectionFor("u-1", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": recipient})
	hub.deliverWorkspaceBroadcast(redisWorkspaceBroadcast{
		Type: notificationCreatedBroadcastType, WorkspaceID: "ws-1", UserIDs: []string{"u-1"},
		Payload: json.RawMessage(`{"type":"notification:created","payload":{}}`), ReplicaID: "other",
	})
	if len(recipient.Send) != 1 {
		t.Fatal("a push relayed through Redis must reach the local recipient")
	}
}

func TestNotificationPushWithoutRecipientsSendsNothing(t *testing.T) {
	conn := connectionFor("u-1", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": conn})
	hub.PushNotification("ws-1", nil, notification.Item{ID: "n"})
	var nilHub *ConversationHub
	nilHub.PushNotification("ws-1", []string{"u-1"}, notification.Item{ID: "n"})
	if len(conn.Send) != 0 {
		t.Fatal("no recipients, no message")
	}
}
