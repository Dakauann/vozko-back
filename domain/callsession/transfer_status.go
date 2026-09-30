package callsession

const CallSessionTransferStatus = "call:transfer_status"

const (
	TransferStatusRinging   = "ringing"
	TransferStatusQueued    = "queued"
	TransferStatusConnected = "connected"
	TransferStatusReturned  = "returned"
	TransferStatusEnded     = "ended"
)

type TransferStatus struct {
	TransferID   string `json:"transfer_id"`
	CallID       string `json:"call_id"`
	Status       string `json:"status"`
	TargetUserID string `json:"target_user_id,omitempty"`
	TargetName   string `json:"target_name,omitempty"`
	QueueID      string `json:"queue_id,omitempty"`
	QueueName    string `json:"queue_name,omitempty"`
	Reason       string `json:"reason,omitempty"`
}
