package billing

import "time"

type CallCompletedEvent struct {
	CallID      string     `json:"callId"`
	WorkspaceID string     `json:"workspaceId"`
	AgentID     *string    `json:"agentId,omitempty"`
	LeadID      *string    `json:"leadId,omitempty"`
	CallSource  CallSource `json:"callSource"`

	CallStart   time.Time `json:"callStart"`
	CallEnd     time.Time `json:"callEnd"`
	DurationSec int       `json:"durationSec"`

	RecordingURL string `json:"recordingUrl"`
	RecordingKey string `json:"recordingKey"`

	CallRecordID string `json:"callRecordId,omitempty"`
}
