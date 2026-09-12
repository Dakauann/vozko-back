package business_metrics

const (
	Exchange = "business_metrics_exchange"

	BusinessMetricsTopic = "business_metrics"
)

type MetricQueueMessage struct {
	EventType  MetricEventType   `json:"event_type"`
	EntityID   string            `json:"entity_id"`
	EntityType EntityType        `json:"entity_type"`
	UserID     *string           `json:"user_id,omitempty"`
	Metadata   map[string]string `json:"metadata"`
}
