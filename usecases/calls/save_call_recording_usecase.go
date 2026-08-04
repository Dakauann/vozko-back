package calls_usecase

import (
	"fmt"
	"log"
	"time"

	"vozko/domain/calls"
	"vozko/domain/calls/billing"
	"vozko/domain/calls/recordings"
	"vozko/domain/media"
)

type saveCallRecordingUseCase struct {
	recordingRepo recordings.CallRecordRepository
	billingRepo   billing.Repository
	fileStorage   media.FileStorage
}

func NewSaveCallRecordingUseCase(
	recordingRepo recordings.CallRecordRepository,
	billingRepo billing.Repository,
	fileStorage media.FileStorage,
) calls.SaveCallRecordingUseCase {
	return &saveCallRecordingUseCase{
		recordingRepo: recordingRepo,
		billingRepo:   billingRepo,
		fileStorage:   fileStorage,
	}
}

func (uc *saveCallRecordingUseCase) SaveRecording(input calls.SaveRecordingInput) (*recordings.CallRecord, error) {
	if input.CallID == "" {
		return nil, fmt.Errorf("call id is required")
	}
	if input.CallStart.IsZero() || input.CallEnd.IsZero() {
		return nil, fmt.Errorf("call start and end times are required")
	}

	var recordingURL string
	if input.RecordingKey != "" {
		recordingURL = uc.fileStorage.GetFileURL(input.RecordingKey)
	} else if len(input.RecordingData) > 0 {
		timestamp := time.Now().Format("20060102_150405")
		recordingKey := fmt.Sprintf("recordings/%s_%s.wav", input.CallID, timestamp)
		if err := uc.fileStorage.UploadFile(recordingKey, input.RecordingData, "audio/wav"); err != nil {
			return nil, fmt.Errorf("failed to upload recording to storage: %w", err)
		}
		recordingURL = uc.fileStorage.GetFileURL(recordingKey)
	} else {
		return nil, fmt.Errorf("recording data or key is required")
	}

	durationSec := int(input.CallEnd.Sub(input.CallStart).Seconds())

	record := &recordings.CallRecord{
		CallID:       input.CallID,
		WorkspaceID:  input.WorkspaceID,
		EntryID:      input.EntryID,
		LeadID:       input.LeadID,
		RecordingURL: recordingURL,
		DurationSec:  durationSec,
		FileSize:     input.FileSize,
		CallStart:    input.CallStart,
		CallEnd:      input.CallEnd,
		CreatedAt:    time.Now(),
		LastUpdated:  time.Now(),
	}

	err := uc.recordingRepo.Save(record)
	if err != nil {
		return nil, fmt.Errorf("failed to save call record to database: %w", err)
	}

	if uc.billingRepo != nil {
		billingRecord, err := uc.billingRepo.GetByCallID(input.CallID)
		if err == nil && billingRecord != nil {
			billingRecord.RecordingURL = &recordingURL
			if err := uc.billingRepo.Update(billingRecord); err != nil {
				log.Printf("recording: failed to link recording URL to billing record %s: %v", input.CallID, err)
			}
		}
	}

	return record, nil
}
