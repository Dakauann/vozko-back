package recordings

import "time"

type RecordingUploadEvent struct {
	CallID       string    `json:"callId"`
	WorkspaceID  string    `json:"workspaceId"`
	EntryID      string    `json:"entryId,omitempty"`
	LeadID       string    `json:"leadId,omitempty"`
	RecordingKey string    `json:"recordingKey"`
	FileSize     int64     `json:"fileSize"`
	CallStart    time.Time `json:"callStart"`
	CallEnd      time.Time `json:"callEnd"`
}

type CallRecord struct {
	CallID       string
	CallRecordID string
	WorkspaceID  string
	CallStart    time.Time
	CallEnd      time.Time
	RecordingURL string
	DurationSec  int
	FileSize     int64
	LastUpdated  time.Time
	CreatedAt    time.Time
	EntryID      string
	LeadID       string
}
