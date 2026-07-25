package business_metrics_usecase

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"vozko/domain/business_metrics"
	"vozko/domain/messaging"
	"vozko/domain/metrics"
)

func extractErrorType(err error) string {
	if err == nil {
		return "unknown"
	}
	errStr := err.Error()

	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
		return "timeout"
	}
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "refused") {
		return "connection"
	}
	if strings.Contains(errStr, "constraint") || strings.Contains(errStr, "unique") {
		return "constraint_violation"
	}
	if strings.Contains(errStr, "not found") || strings.Contains(errStr, "404") {
		return "not_found"
	}
	if strings.Contains(errStr, "invalid") || strings.Contains(errStr, "bad") {
		return "validation"
	}

	return "database_error"
}

type consumeMetricUseCase struct {
	queueSub       messaging.MessageQueueSub
	repo           business_metrics.Repository
	metricsService metrics.MetricsService
	semaphore      chan struct{}
}

func NewConsumeMetricUseCase(
	queueSub messaging.MessageQueueSub,
	repo business_metrics.Repository,
	metricsService metrics.MetricsService,
) business_metrics.ConsumeMetricUseCase {
	return &consumeMetricUseCase{
		queueSub:       queueSub,
		repo:           repo,
		metricsService: metricsService,
		semaphore:      make(chan struct{}, 10),
	}
}

func (uc *consumeMetricUseCase) Start() error {
	go func() {
		if err := uc.queueSub.Subscribe(business_metrics.BusinessMetricsTopic, func(payload []byte, ack messaging.MessageAck) {
			uc.handleMetric(payload, ack)
		}); err != nil {
			log.Printf("Failed to subscribe to business metrics queue: %v", err)
		}
	}()
	return nil
}

func (uc *consumeMetricUseCase) handleMetric(payload []byte, ack messaging.MessageAck) {
	var message business_metrics.MetricQueueMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		log.Printf("Failed to unmarshal metric message: %v", err)
		_ = ack.Nack(false)
		return
	}
	uc.semaphore <- struct{}{}
	go func(msg business_metrics.MetricQueueMessage) {
		defer func() { <-uc.semaphore }()
		defer func() {
			if err := ack.Ack(); err != nil {
				log.Printf("Failed to ack metric message: %v", err)
			}
		}()
		metric := &business_metrics.Metric{
			EventType:  message.EventType,
			EntityID:   message.EntityID,
			EntityType: message.EntityType,
			UserID:     message.UserID,
			Metadata:   message.Metadata,
			OccurredAt: time.Now(),
		}

		if err := uc.repo.Record(metric); err != nil {
			log.Printf("Failed to save metric %s for entity %s: %v", message.EventType, message.EntityID, err)
			if uc.metricsService != nil {
				uc.metricsService.IncMetricsRecordError(string(message.EventType), extractErrorType(err))
			}
			return
		}
	}(message)

}
