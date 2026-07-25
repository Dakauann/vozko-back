package calls

type CallStatus string

const (
	CallStatusPending   CallStatus = "PENDING"
	CallStatusRinging   CallStatus = "RINGING"
	CallStatusOngoing   CallStatus = "ONGOING"
	CallStatusCompleted CallStatus = "COMPLETED"
	CallStatusMissed    CallStatus = "MISSED"
)
