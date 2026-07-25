package business_metrics

import "time"

type MetricEventType string

const (
	EventUserAccountCreated MetricEventType = "user_account_created"
	EventUserLogin          MetricEventType = "user_login"

	EventInsuranceQuoteRequested MetricEventType = "insurance_quote_requested"
	EventInsuranceQuoteCreated   MetricEventType = "insurance_quote_created"

	EventCampaignStarted MetricEventType = "campaign_started"
	EventCampaignStopped MetricEventType = "campaign_stopped"

	EventCallStarted MetricEventType = "call_started"
	EventCallEnded   MetricEventType = "call_ended"

	EventWhatsAppMessageSent         MetricEventType = "whatsapp_message_sent"
	EventWhatsAppTemplateMessageSent MetricEventType = "whatsapp_template_message_sent"

	EventEmailSent MetricEventType = "email_sent"
)

type EntityType string

const (
	EntityTypeUser           EntityType = "user"
	EntityTypeInsuranceQuote EntityType = "insurance_quote"
	EntityTypeCampaign       EntityType = "campaign"
	EntityTypeCall           EntityType = "call"
	EntityTypeMessage        EntityType = "message"
	EntityTypeEmail          EntityType = "email"
)

type Metric struct {
	ID         string            `json:"id"`
	EventType  MetricEventType   `json:"event_type"`
	EntityID   string            `json:"entity_id"`
	EntityType EntityType        `json:"entity_type"`
	UserID     *string           `json:"user_id"`
	Metadata   map[string]string `json:"metadata"`
	OccurredAt time.Time         `json:"occurred_at"`
	CreatedAt  time.Time         `json:"created_at"`
}
