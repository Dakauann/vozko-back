package calls_usecase

import (
	"encoding/json"
	"log"

	"vozko/domain/calls"
	"vozko/domain/calls/recordings"
	"vozko/domain/messaging"
)

type ConsumeRecordingUploadUseCase struct {
	subscriber messaging.MessageQueueSub
	saveUC     calls.SaveCallRecordingUseCase
	logger     *log.Logger
}

func NewConsumeRecordingUploadUseCase(
	subscriber messaging.MessageQueueSub,
	saveUC calls.SaveCallRecordingUseCase,
	logger *log.Logger,
) *ConsumeRecordingUploadUseCase {
	if logger == nil {
		logger = log.Default()
	}
	return &ConsumeRecordingUploadUseCase{
		subscriber: subscriber,
		saveUC:     saveUC,
		logger:     logger,
	}
}

func (c *ConsumeRecordingUploadUseCase) Start() error {
	return c.subscriber.Subscribe(recordings.TopicRecordingUpload, func(message []byte, ack messaging.MessageAck) {
		var event recordings.RecordingUploadEvent
		if err := json.Unmarshal(message, &event); err != nil {
			c.logger.Printf("recording-upload consumer: bad message, nacking: %v", err)
			_ = ack.Nack(false)
			return
		}

		_, err := c.saveUC.SaveRecording(calls.SaveRecordingInput{
			CallID:       event.CallID,
			WorkspaceID:  event.WorkspaceID,
			EntryID:      event.EntryID,
			LeadID:       event.LeadID,
			RecordingKey: event.RecordingKey,
			FileSize:     event.FileSize,
			CallStart:    event.CallStart,
			CallEnd:      event.CallEnd,
		})
		if err != nil {
			c.logger.Printf("recording-upload consumer: upload failed for call %s (attempt %d): %v",
				event.CallID, ack.DeliveryCount(), err)
			_ = ack.Nack(true)
			return
		}

		c.logger.Printf("recording-upload consumer: call %s uploaded successfully", event.CallID)
		_ = ack.Ack()
	})
}
