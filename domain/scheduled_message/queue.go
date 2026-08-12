package scheduled_message

// TopicFire carries a due scheduled message to whichever replica picks it up.
const TopicFire = "scheduled_message.fire"

// FireMessage is the queue payload.
//
// It carries the id and nothing else that matters: every field the dispatcher
// needs is re-read from the row at fire time. A payload that carried the text
// would deliver whatever was true when it was enqueued, which is precisely the
// bug a cancelled or rescheduled message would expose.
type FireMessage struct {
	ID     string `json:"id"`
	FireAt int64  `json:"fire_at"`
}
