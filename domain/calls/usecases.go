package calls

import (
	"time"

	"vozko/domain/calls/recordings"
)

type SaveCallRecordingUseCase interface {
	SaveRecording(input SaveRecordingInput) (*recordings.CallRecord, error)
}

type SaveRecordingInput struct {
	CallID        string
	WorkspaceID   string
	EntryID       string
	LeadID        string
	RecordingData []byte
	RecordingKey  string
	FileSize      int64
	CallStart     time.Time
	CallEnd       time.Time
}
