package notification

const (
	Exchange = "notifications_exchange"

	EmailNotificationTopic = "notification_emails"
	FanoutTopic            = "notification.fanout"
	BroadcastTopic         = "notification.broadcast"
)

type NotificationQueueMessage struct {
	Email        string                 `json:"email"`
	Template     string                 `json:"template"`
	Subject      string                 `json:"subject"`
	Placeholders map[string]interface{} `json:"placeholders"`
	Attempt      int                    `json:"attempt,omitempty"`
}

type FanoutMessage struct {
	NotificationID string `json:"notificationId"`
}

type BroadcastMessage struct {
	BroadcastID string `json:"broadcastId"`
}
