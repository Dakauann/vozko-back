// Package crm_telemetry defines the async CRM operational telemetry bus.
// Hot paths must only Publish; consumers persist to Postgres (replica-safe, no DB hammering).
package crm_telemetry

import "time"

const (
	Topic = "crm_telemetry"
	// MaxDeliveryAttempts before dead-letter drop (Nack without requeue).
	MaxDeliveryAttempts = 5
)

type Kind string

const (
	KindConversationEvent Kind = "conversation_event"
	KindAssignmentHistory Kind = "assignment_history"
	KindAISession         Kind = "ai_session"
	KindQueueEvent        Kind = "queue_event"
	KindPresence          Kind = "presence"
)

// Envelope is the Rabbit payload. Payload is kind-specific JSON.
type Envelope struct {
	// ID is the idempotency key for this envelope (UUID). Consumer dedupes on it.
	ID         string    `json:"id"`
	Kind       Kind      `json:"kind"`
	Payload    []byte    `json:"payload"`
	OccurredAt time.Time `json:"occurred_at"`
}

// PresencePayload is a human attendant presence transition.
type PresencePayload struct {
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	State       string    `json:"state"` // online | offline | on_call | wrap_up
	Source      string    `json:"source"`
	At          time.Time `json:"at"`
}

// AssignmentHistoryPayload opens a new ownership interval (consumer closes any open one first).
type AssignmentHistoryPayload struct {
	ID                string    `json:"id,omitempty"` // becomes assignment_history.id when set
	WorkspaceID       string    `json:"workspace_id"`
	EntryID           string    `json:"entry_id"`
	EntryType         string    `json:"entry_type"`
	ActorKind         string    `json:"actor_kind"`
	AssignedActorID   string    `json:"assigned_actor_id"`
	PreviousActorID   string    `json:"previous_actor_id,omitempty"`
	Trigger           string    `json:"trigger"`
	AssignedByActorID string    `json:"assigned_by_actor_id,omitempty"`
	BusinessPhoneID   string    `json:"business_phone_id,omitempty"`
	SIPTrunkID        string    `json:"sip_trunk_id,omitempty"`
	DepartmentID      string    `json:"department_id,omitempty"`
	StartedAt         time.Time `json:"started_at"`
}

// AISessionOp is a deferred AI attendance mutation.
type AISessionOp string

const (
	AISessionOpRecordReply  AISessionOp = "record_ai_reply"
	AISessionOpEndOpen      AISessionOp = "end_open"
	AISessionOpTouchInbound AISessionOp = "touch_inbound"
	AISessionOpEnsureOpen   AISessionOp = "ensure_open"
)

// AISessionPayload is processed by the consumer against ai_attendance_sessions.
type AISessionPayload struct {
	Op                  AISessionOp `json:"op"`
	WorkspaceID         string      `json:"workspace_id"`
	EntryID             string      `json:"entry_id"`
	EntryType           string      `json:"entry_type"`
	AgentID             string      `json:"agent_id,omitempty"`
	Channel             string      `json:"channel,omitempty"`
	CallID              string      `json:"call_id,omitempty"`
	CampaignID          string      `json:"campaign_id,omitempty"`
	Model               string      `json:"model,omitempty"`
	MessageID           string      `json:"message_id,omitempty"`
	Outcome             string      `json:"outcome,omitempty"`
	Reason              string      `json:"reason,omitempty"`
	HandoffTargetUserID string      `json:"handoff_target_user_id,omitempty"`
}

// QueueEventPayload mirrors dialer queue lifecycle for durable SLA stats.
type QueueEventPayload struct {
	ID          string    `json:"id,omitempty"`
	WorkspaceID string    `json:"workspace_id"`
	TransferID  string    `json:"transfer_id,omitempty"`
	CallID      string    `json:"call_id,omitempty"`
	TargetKind  string    `json:"target_kind,omitempty"`
	TargetID    string    `json:"target_id,omitempty"`
	Type        string    `json:"type"`
	Position    int       `json:"position"`
	WaitedMS    int64     `json:"waited_ms"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// Publisher is the only telemetry surface allowed on hot paths.
// Implementations must not perform Postgres writes.
type Publisher interface {
	// Publish enqueues telemetry. Failures must not fail the caller (log + return).
	Publish(kind Kind, payload any) error
}

// Consumer starts the durable worker.
type Consumer interface {
	Start() error
}

// DropRecorder records publish/consume drops for ops (optional Prometheus hook).
type DropRecorder interface {
	IncTelemetryPublishError(kind string)
	IncTelemetryConsumeError(kind, reason string)
	IncTelemetryDropped(kind, reason string)
}
