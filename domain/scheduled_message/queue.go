package scheduled_message

const (
	Exchange = "scheduled_message_exchange"

	TopicFire = "scheduled_message.fire"
)

type FireMessage struct {
	ID     string `json:"id"`
	FireAt int64  `json:"fire_at"`
}
