package conversation_event_usecase

import (
	"log"

	"github.com/google/uuid"

	ce "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
)

type logger struct {
	pub crm_telemetry.Publisher
}

func NewLogger(pub crm_telemetry.Publisher) ce.Logger {
	return &logger{pub: pub}
}

func (l *logger) Log(event *ce.ConversationEvent) {
	if event == nil || l.pub == nil {
		return
	}
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	event.Normalize()
	if err := l.pub.Publish(crm_telemetry.KindConversationEvent, event); err != nil {
		log.Printf("[ConversationEventLogger] publish %s for entry %s: %v",
			event.EventType, event.EntryID, err)
	}
}
