package conversation

import "context"

type CallSource interface {
	Dial(ctx context.Context, input CallDialInput) (CRMCall, error)
	Name() string
}

type CallDialInput struct {
	PhoneNumber string
	EntryID     string
	EntryType   string
	UserID      string
	WorkspaceID string
	IsAdmin     bool

	WhatsAppPhoneID string
	TrunkID         string
}

type CRMCall interface {
	ID() string
	SendAudio(pcm16 []byte) error
	AudioStream() <-chan []byte
	Events() <-chan CallEvent
	Hangup() error
	Done() <-chan struct{}
}

type CallEventType string

const (
	CallEventRinging  CallEventType = "ringing"
	CallEventAlerting CallEventType = "alerting"
	CallEventAnswered CallEventType = "answered"
	CallEventEnded    CallEventType = "ended"
	CallEventFailed   CallEventType = "failed"
	CallEventBusy     CallEventType = "busy"
	CallEventNoAnswer CallEventType = "no_answer"
	CallEventDeclined CallEventType = "declined"
)

type CallEvent struct {
	Type   CallEventType `json:"type"`
	Reason string        `json:"reason,omitempty"`
}

func (e CallEvent) IsTerminal() bool {
	switch e.Type {
	case CallEventEnded, CallEventFailed, CallEventBusy, CallEventNoAnswer, CallEventDeclined:
		return true
	}
	return false
}

func PendingOutcome(events <-chan CallEvent) (CallEvent, bool) {
	for {
		select {
		case event, open := <-events:
			if !open {
				return CallEvent{}, false
			}
			if event.IsTerminal() {
				return event, true
			}
		default:
			return CallEvent{}, false
		}
	}
}
