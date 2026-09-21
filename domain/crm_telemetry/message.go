package crm_telemetry

import "time"

const (
	Exchange = "crm_telemetry_exchange"

	Topic               = "crm_telemetry"
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

type Envelope struct {
	ID         string    `json:"id"`
	Kind       Kind      `json:"kind"`
	Payload    []byte    `json:"payload"`
	OccurredAt time.Time `json:"occurred_at"`
}

type PresencePayload struct {
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	State       string    `json:"state"`
	Source      string    `json:"source"`
	At          time.Time `json:"at"`
}

type AssignmentHistoryPayload struct {
	ID                string    `json:"id,omitempty"`
	WorkspaceID       string    `json:"workspace_id"`
	EntryID           string    `json:"entry_id"`
	EntryType         string    `json:"entry_type"`
	ActorKind         string    `json:"actor_kind"`
	AssignedActorID   string    `json:"assigned_actor_id"`
	PreviousActorID   string    `json:"previous_actor_id,omitempty"`
	Trigger           string    `json:"trigger"`
	AssignedByActorID string    `json:"assigned_by_actor_id,omitempty"`
	BusinessPhoneID   string    `json:"business_phone_id,omitempty"`
	DepartmentID      string    `json:"department_id,omitempty"`
	StartedAt         time.Time `json:"started_at"`
}

type AISessionOp string

const (
	AISessionOpRecordReply  AISessionOp = "record_ai_reply"
	AISessionOpEndOpen      AISessionOp = "end_open"
	AISessionOpTouchInbound AISessionOp = "touch_inbound"
	AISessionOpEnsureOpen   AISessionOp = "ensure_open"
)

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

type Publisher interface {
	Publish(kind Kind, payload any) error
}

type Consumer interface {
	Start() error
}

type DropRecorder interface {
	IncTelemetryPublishError(kind string)
	IncTelemetryConsumeError(kind, reason string)
	IncTelemetryDropped(kind, reason string)
}
