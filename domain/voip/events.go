package voip

type EventType string

const (
	EventIncomingInvite EventType = "INCOMING_INVITE"
	EventCallRinging    EventType = "CALL_RINGING"
	EventCallAccepted   EventType = "CALL_ACCEPTED"
	EventCallEnded      EventType = "CALL_ENDED"
	EventMediaReady     EventType = "MEDIA_READY"
	EventError          EventType = "ERROR"
)

type Event struct {
	Type    EventType
	CallID  CallID
	Payload map[string]interface{}
}
