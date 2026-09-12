package notification

const (
	Exchange = "notifications_exchange"

	EmailNotificationTopic = "notification_emails"
)

type NotificationQueueMessage struct {
	Email        string                 `json:"email"`
	Template     string                 `json:"template"`
	Subject      string                 `json:"subject"`
	Placeholders map[string]interface{} `json:"placeholders"`
}
