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
	SIPTrunkID  string

	WhatsAppPhoneID string
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
