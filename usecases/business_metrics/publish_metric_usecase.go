package business_metrics_usecase

import (
	"encoding/json"
	"fmt"

	"vozko/domain/business_metrics"
	"vozko/domain/messaging"
)

type publishMetricUseCase struct {
	queuePub messaging.MessageQueuePub
}

func NewPublishMetricUseCase(queuePub messaging.MessageQueuePub) business_metrics.RecordMetricUseCase {
	return &publishMetricUseCase{
		queuePub: queuePub,
	}
}

func (uc *publishMetricUseCase) Execute(input business_metrics.RecordMetricInput) error {

	message := business_metrics.MetricQueueMessage{
		EventType:  input.EventType,
		EntityID:   input.EntityID,
		EntityType: input.EntityType,
		UserID:     input.UserID,
		Metadata:   input.Metadata,
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal metric message: %w", err)
	}

	if err := uc.queuePub.Publish(business_metrics.BusinessMetricsTopic, payload); err != nil {
		return fmt.Errorf("failed to publish metric to queue: %w", err)
		// TODO: make this pass the error to prometheus logging
	}

	return nil
}
